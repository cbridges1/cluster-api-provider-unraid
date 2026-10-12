// Package controller reconciles UnraidClusters and UnraidControlPlanes.
package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cbridges1/cluster-api-provider-unraid/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-unraid/internal/sshx"
)

// ClusterNameLabel is the label Cluster API puts on a cluster's objects.
const ClusterNameLabel = "cluster.x-k8s.io/cluster-name"

var clusterGVK = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Cluster"}

// errNotReady means "come back later", not a failure.
var errNotReady = errors.New("not ready")

// ownerCluster returns the Cluster API Cluster an object belongs to, by its
// cluster-name label, or nil if it isn't labeled (yet).
func ownerCluster(ctx context.Context, c client.Client, obj metav1.Object) (*unstructured.Unstructured, error) {
	name := obj.GetLabels()[ClusterNameLabel]
	if name == "" {
		return nil, nil
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

// paused reports whether reconciliation is paused for the object or its
// Cluster.
func paused(cluster *unstructured.Unstructured, obj metav1.Object) bool {
	if _, ok := obj.GetAnnotations()["cluster.x-k8s.io/paused"]; ok {
		return true
	}
	if cluster == nil {
		return false
	}
	p, _, _ := unstructured.NestedBool(cluster.Object, "spec", "paused")
	return p
}

// credentials reads an UnraidCluster's SSH credentials Secret.
func credentials(ctx context.Context, c client.Client, uc *v1alpha1.UnraidCluster) (sshx.Credentials, error) {
	var s corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: uc.Namespace, Name: uc.Spec.CredentialsRef.Name}, &s); err != nil {
		return sshx.Credentials{}, fmt.Errorf("credentials Secret %s: %w", uc.Spec.CredentialsRef.Name, err)
	}
	cr := sshx.Credentials{
		Username:   string(s.Data["username"]),
		Password:   string(s.Data["password"]),
		PrivateKey: s.Data["privateKey"],
	}
	if cr.Username == "" {
		return cr, fmt.Errorf("credentials Secret %s has no username", s.Name)
	}
	return cr, nil
}

// dial connects to an UnraidCluster's server, pinning its host key.
func dial(ctx context.Context, c client.Client, uc *v1alpha1.UnraidCluster) (*sshx.Client, error) {
	cr, err := credentials(ctx, c, uc)
	if err != nil {
		return nil, err
	}
	key := uc.Spec.HostKey
	if key == "" {
		key = uc.Status.HostKey
	}
	return sshx.Dial(ctx, sshx.Target{Host: uc.Spec.Host, Port: uc.Spec.SSHPort, HostKey: key}, cr)
}

func condition(status bool, reason, msg string) metav1.Condition {
	s := metav1.ConditionFalse
	if status {
		s = metav1.ConditionTrue
	}
	return metav1.Condition{Type: v1alpha1.ReadyCondition, Status: s, Reason: reason, Message: msg}
}

func ptr[T any](v T) *T { return &v }
