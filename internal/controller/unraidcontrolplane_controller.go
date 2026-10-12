package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cbridges1/cluster-api-provider-unraid/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-unraid/internal/k3s"
	"github.com/cbridges1/cluster-api-provider-unraid/internal/sshx"
)

// Finalizer keeps an UnraidControlPlane until its container is removed.
const Finalizer = "unraidcontrolplane.infrastructure.cluster.x-k8s.io/finalizer"

// UnraidControlPlaneReconciler runs the k3s server container on the
// UnraidCluster's server, and publishes the cluster's kubeconfig.
type UnraidControlPlaneReconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=unraidcontrolplanes;unraidcontrolplanetemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=unraidcontrolplanes/status;unraidcontrolplanes/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements reconcile.Reconciler.
func (r *UnraidControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cp v1alpha1.UnraidControlPlane
	if err := r.Client.Get(ctx, req.NamespacedName, &cp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	cluster, err := ownerCluster(ctx, r.Client, &cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if paused(cluster, &cp) {
		return ctrl.Result{}, nil
	}
	if !cp.DeletionTimestamp.IsZero() {
		return r.delete(ctx, &cp, cluster)
	}
	if controllerutil.AddFinalizer(&cp, Finalizer) {
		if err := r.Client.Update(ctx, &cp); err != nil {
			return ctrl.Result{}, err
		}
	}
	if cluster == nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Status goes out as a merge patch: Cluster API's topology controller
	// edits these objects too, and full updates would conflict.
	base := cp.DeepCopy()
	res, err := r.reconcile(ctx, &cp, cluster)
	if err != nil && !errors.Is(err, errNotReady) {
		v1alpha1.SetCondition(&cp.Status.Conditions, condition(false, "Error", err.Error()))
		_ = r.Client.Status().Patch(ctx, &cp, client.MergeFrom(base))
		log.FromContext(ctx).Error(err, "reconcile failed")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if perr := r.Client.Status().Patch(ctx, &cp, client.MergeFrom(base)); perr != nil {
		return ctrl.Result{}, perr
	}
	return res, nil
}

// infraCluster returns the Cluster's UnraidCluster.
func (r *UnraidControlPlaneReconciler) infraCluster(ctx context.Context, cluster *unstructured.Unstructured) (*v1alpha1.UnraidCluster, error) {
	name, _, _ := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "name")
	if name == "" {
		return nil, fmt.Errorf("%w: Cluster has no infrastructureRef yet", errNotReady)
	}
	var uc v1alpha1.UnraidCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: cluster.GetNamespace(), Name: name}, &uc); err != nil {
		return nil, err
	}
	return &uc, nil
}

func (r *UnraidControlPlaneReconciler) reconcile(ctx context.Context, cp *v1alpha1.UnraidControlPlane, cluster *unstructured.Unstructured) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	wait := func(reason, msg string) (ctrl.Result, error) {
		v1alpha1.SetCondition(&cp.Status.Conditions, condition(false, reason, msg))
		return ctrl.Result{RequeueAfter: 10 * time.Second}, errNotReady
	}
	uc, err := r.infraCluster(ctx, cluster)
	if err != nil {
		if errors.Is(err, errNotReady) {
			return wait("WaitingForInfrastructure", err.Error())
		}
		return ctrl.Result{}, err
	}
	if uc.Status.Initialization.Provisioned == nil || !*uc.Status.Initialization.Provisioned {
		return wait("WaitingForInfrastructure", "UnraidCluster "+uc.Name+" isn't provisioned (SSH) yet")
	}

	// The endpoint is the server itself (host network).
	want := v1alpha1.APIEndpoint{Host: uc.Spec.Host, Port: k3s.APIPort}
	if cp.Spec.ControlPlaneEndpoint != want {
		patch := client.MergeFrom(cp.DeepCopy())
		cp.Spec.ControlPlaneEndpoint = want
		if err := r.Client.Patch(ctx, cp, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	sc, err := dial(ctx, r.Client, uc)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ssh %s: %w", uc.Spec.Host, err)
	}
	defer sc.Close()

	name := k3s.ContainerName(&cp.Spec)
	image := k3s.Image(&cp.Spec)
	out, err := sc.MustRun(ctx, k3s.InspectCommand(name), nil)
	if err != nil {
		return ctrl.Result{}, err
	}
	fields := strings.Fields(out)
	switch {
	case len(fields) == 0:
		// Host network: one k3s per server. Don't fight another one.
		if r2, err := sc.Run(ctx, k3s.PortBusyCommand, nil); err == nil && r2.ExitCode == 0 {
			return ctrl.Result{}, fmt.Errorf("port %d is already in use on %s (another k3s?) — one Unraid control plane per server", k3s.APIPort, uc.Spec.Host)
		}
		lg.Info("starting k3s", "host", uc.Spec.Host, "container", name, "image", image)
		if err := r.start(ctx, sc, cp, uc.Spec.Host); err != nil {
			return ctrl.Result{}, err
		}
		return wait("Starting", "started container "+name+" ("+image+")")
	case fields[0] != image:
		// Version change: same data, new image.
		lg.Info("replacing k3s container for a new image", "from", fields[0], "to", image)
		if _, err := sc.MustRun(ctx, "docker rm -f "+k3s.Quote(name), nil); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.start(ctx, sc, cp, uc.Spec.Host); err != nil {
			return ctrl.Result{}, err
		}
		return wait("Upgrading", "replaced container "+name+" with "+image)
	case len(fields) > 1 && fields[1] != "running":
		if _, err := sc.MustRun(ctx, "docker start "+k3s.Quote(name), nil); err != nil {
			return ctrl.Result{}, err
		}
		return wait("Starting", "started stopped container "+name)
	}

	nodes, _ := sc.MustRun(ctx, k3s.NodesCommand(name), nil)
	if !k3s.AllReady(nodes) {
		return wait("WaitingForNodes", "waiting for the node to be Ready")
	}
	raw, err := sc.MustRun(ctx, k3s.KubeconfigCommand(name), nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reading kubeconfig: %w", err)
	}
	clusterName := cluster.GetName()
	kc, err := k3s.Kubeconfig(raw, uc.Spec.Host, clusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("kubeconfig: %w", err)
	}
	if err := r.writeKubeconfig(ctx, cp, clusterName, kc); err != nil {
		return ctrl.Result{}, err
	}

	cp.Status.Initialization.ControlPlaneInitialized = ptr(true)
	cp.Status.Version = cp.Spec.Version
	cp.Status.Image = image
	v1alpha1.SetCondition(&cp.Status.Conditions, condition(true, "Running", "k3s "+image+" running on "+uc.Spec.Host))
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// start creates the config, clears stale cgroups and runs the container.
func (r *UnraidControlPlaneReconciler) start(ctx context.Context, sc *sshx.Client, cp *v1alpha1.UnraidControlPlane, host string) error {
	data, etc := k3s.Paths(&cp.Spec)
	if _, err := sc.MustRun(ctx, "mkdir -p "+k3s.Quote(data)+" "+k3s.Quote(etc), nil); err != nil {
		return err
	}
	reg, err := k3s.RegistriesYAML(&cp.Spec)
	if err != nil {
		return err
	}
	if reg != "" {
		if _, err := sc.MustRun(ctx, "cat > "+k3s.Quote(etc+"/registries.yaml"), strings.NewReader(reg)); err != nil {
			return err
		}
	}
	if _, err := sc.MustRun(ctx, k3s.CleanStaleCgroups, nil); err != nil {
		return err
	}
	_, err = sc.MustRun(ctx, k3s.RunCommand(&cp.Spec, host), nil)
	return err
}

// writeKubeconfig creates or updates the <cluster>-kubeconfig Secret Cluster
// API reads.
func (r *UnraidControlPlaneReconciler) writeKubeconfig(ctx context.Context, cp *v1alpha1.UnraidControlPlane, clusterName string, kc []byte) error {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-kubeconfig", Namespace: cp.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, s, func() error {
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		s.Labels[ClusterNameLabel] = clusterName
		s.Type = "cluster.x-k8s.io/secret"
		s.Data = map[string][]byte{"value": kc}
		return controllerutil.SetOwnerReference(cp, s, r.Client.Scheme())
	})
	return err
}

func (r *UnraidControlPlaneReconciler) delete(ctx context.Context, cp *v1alpha1.UnraidControlPlane, cluster *unstructured.Unstructured) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(cp, Finalizer) {
		return ctrl.Result{}, nil
	}
	if cluster != nil {
		uc, err := r.infraCluster(ctx, cluster)
		if err == nil {
			sc, err := dial(ctx, r.Client, uc)
			if err != nil {
				lg.Info("can't reach server to remove k3s; retrying", "error", err.Error())
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			defer sc.Close()
			name := k3s.ContainerName(&cp.Spec)
			if _, err := sc.MustRun(ctx, "docker rm -f "+k3s.Quote(name)+" >/dev/null 2>&1; true", nil); err != nil {
				return ctrl.Result{}, err
			}
			if cp.Spec.DeletePolicy == v1alpha1.DeletePolicyDelete {
				data, etc := k3s.Paths(&cp.Spec)
				if !strings.HasPrefix(data, "/mnt/") && !strings.HasPrefix(data, "/var/lib/") {
					return ctrl.Result{}, fmt.Errorf("refusing to delete data outside /mnt or /var/lib: %s", data)
				}
				if _, err := sc.MustRun(ctx, "rm -rf "+k3s.Quote(data)+" "+k3s.Quote(etc), nil); err != nil {
					return ctrl.Result{}, err
				}
			}
			lg.Info("removed k3s container", "host", uc.Spec.Host, "container", name, "data", cp.Spec.DeletePolicy)
		}
	}
	controllerutil.RemoveFinalizer(cp, Finalizer)
	return ctrl.Result{}, r.Client.Update(ctx, cp)
}

// SetupWithManager registers the reconciler.
func (r *UnraidControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.UnraidControlPlane{}).Owns(&corev1.Secret{}).Complete(r)
}
