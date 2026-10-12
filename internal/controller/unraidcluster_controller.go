package controller

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cbridges1/cluster-api-provider-unraid/api/v1alpha1"
)

// UnraidClusterReconciler checks the server is reachable over SSH and
// records its host key. There's nothing on the server to create for the
// cluster itself; the control plane does the work.
type UnraidClusterReconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=unraidclusters;unraidclustertemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=unraidclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile implements reconcile.Reconciler.
func (r *UnraidClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	var uc v1alpha1.UnraidCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &uc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !uc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	cluster, err := ownerCluster(ctx, r.Client, &uc)
	if err != nil {
		return ctrl.Result{}, err
	}
	if paused(cluster, &uc) {
		return ctrl.Result{}, nil
	}

	base := uc.DeepCopy()
	sc, err := dial(ctx, r.Client, &uc)
	if err != nil {
		lg.Info("server not reachable over SSH", "host", uc.Spec.Host, "error", err.Error())
		v1alpha1.SetCondition(&uc.Status.Conditions, condition(false, "SSHFailed", err.Error()))
		_ = r.Client.Status().Patch(ctx, &uc, client.MergeFrom(base))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	defer sc.Close()
	if _, err := sc.MustRun(ctx, "docker version --format '{{.Server.Version}}'", nil); err != nil {
		v1alpha1.SetCondition(&uc.Status.Conditions, condition(false, "DockerUnavailable", err.Error()))
		_ = r.Client.Status().Patch(ctx, &uc, client.MergeFrom(base))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if uc.Status.HostKey == "" {
		uc.Status.HostKey = sc.HostKey // trust on first use
	}
	uc.Status.Initialization.Provisioned = ptr(true)
	v1alpha1.SetCondition(&uc.Status.Conditions, condition(true, "Reachable", "SSH and Docker reachable on "+uc.Spec.Host))
	if err := r.Client.Status().Patch(ctx, &uc, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

// SetupWithManager registers the reconciler.
func (r *UnraidClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.UnraidCluster{}).Complete(r)
}
