package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// UnraidClusterSpec says which Unraid server a cluster runs on and how to
// reach it.
type UnraidClusterSpec struct {
	// host is the Unraid server's address (LAN IP or hostname). The cluster's
	// API server is reached here too.
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// sshPort is the server's SSH port.
	// +optional
	// +kubebuilder:default=22
	SSHPort int32 `json:"sshPort,omitempty"`

	// credentialsRef names a Secret in this namespace holding `username` and
	// either `password` or `privateKey` (PEM) for SSH. Unraid has no
	// key-only mode by default, so password auth is the usual choice.
	CredentialsRef SecretRef `json:"credentialsRef"`

	// hostKey pins the server's SSH host key ("<type> <base64>", as in
	// known_hosts). Empty: trust the key seen on first connection and record
	// it in status.hostKey; later connections must match it.
	// +optional
	HostKey string `json:"hostKey,omitempty"`

	// controlPlaneEndpoint is set by the UnraidControlPlane (host:6443).
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`
}

// UnraidClusterInitializationStatus reports Cluster API initialization.
// +kubebuilder:validation:MinProperties=1
type UnraidClusterInitializationStatus struct {
	// provisioned is true once the server is reachable over SSH.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// UnraidClusterStatus is the observed state.
type UnraidClusterStatus struct {
	// +optional
	Initialization UnraidClusterInitializationStatus `json:"initialization,omitempty,omitzero"`
	// hostKey is the SSH host key trusted for this server.
	// +optional
	HostKey string `json:"hostKey,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=unraidclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name"
// +kubebuilder:printcolumn:name="Host",type="string",JSONPath=".spec.host"
// +kubebuilder:printcolumn:name="Provisioned",type="boolean",JSONPath=".status.initialization.provisioned"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// UnraidCluster is the infrastructure for a cluster on an Unraid server.
type UnraidCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UnraidClusterSpec   `json:"spec,omitempty"`
	Status UnraidClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnraidClusterList is a list of UnraidClusters.
type UnraidClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnraidCluster `json:"items"`
}

// UnraidClusterTemplateResource is a template's UnraidCluster.
type UnraidClusterTemplateResource struct {
	// +optional
	ObjectMeta ObjectMeta        `json:"metadata,omitempty,omitzero"`
	Spec       UnraidClusterSpec `json:"spec"`
}

// UnraidClusterTemplateSpec is an UnraidClusterTemplate's spec.
type UnraidClusterTemplateSpec struct {
	Template UnraidClusterTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=unraidclustertemplates,scope=Namespaced,categories=cluster-api

// UnraidClusterTemplate is the ClusterClass template for UnraidClusters.
type UnraidClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec UnraidClusterTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// UnraidClusterTemplateList is a list of UnraidClusterTemplates.
type UnraidClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnraidClusterTemplate `json:"items"`
}

// ObjectMeta is the metadata a template may carry.
type ObjectMeta struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

func init() {
	SchemeBuilder.Register(&UnraidCluster{}, &UnraidClusterList{}, &UnraidClusterTemplate{}, &UnraidClusterTemplateList{})
}
