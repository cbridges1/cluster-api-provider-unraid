package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Delete policies for an UnraidControlPlane's data directory.
const (
	DeletePolicyKeep   = "Keep"
	DeletePolicyDelete = "Delete"
)

// UnraidControlPlaneSpec is the k3s server container: the same `docker run`
// hyve-unraid-k3s-module uses — privileged, host network, the server's
// shares mounted in — so one Unraid server runs one such cluster.
type UnraidControlPlaneSpec struct {
	// version is the Kubernetes version (set from the Cluster's topology),
	// e.g. v1.34.8. The image is rancher/k3s:<version>-k3s1 unless image is set.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// image overrides the k3s image (e.g. rancher/k3s:v1.34.8-k3s2).
	// +optional
	Image string `json:"image,omitempty"`

	// containerName is the k3s container's name on the server.
	// +optional
	// +kubebuilder:default=k3s
	ContainerName string `json:"containerName,omitempty"`

	// dataPath is the directory on the server holding the cluster's state
	// (<dataPath>/<containerName>) and k3s config
	// (<dataPath>/<containerName>-etc-rancher).
	// +optional
	// +kubebuilder:default="/mnt/cache/appdata"
	DataPath string `json:"dataPath,omitempty"`

	// mountShares mounts the server's /mnt/user into the container at the
	// same path, so workloads keep the share access Unraid's own containers
	// have.
	// +optional
	// +kubebuilder:default=true
	MountShares *bool `json:"mountShares,omitempty"`

	// dockerSocket mounts the server's /var/run/docker.sock into the
	// container — what Cluster API's Docker provider needs. Root-equivalent
	// access to the server's Docker for anything that can mount it.
	// +optional
	DockerSocket bool `json:"dockerSocket,omitempty"`

	// disable lists k3s packaged components to turn off.
	// +optional
	// +kubebuilder:default={traefik}
	Disable []string `json:"disable,omitempty"`

	// extraArgs are extra `k3s server` arguments.
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// registryMirrors writes k3s's registries.yaml: registry → mirror
	// endpoint URLs (e.g. a plain-HTTP Harbor on the LAN).
	// +optional
	RegistryMirrors map[string][]string `json:"registryMirrors,omitempty"`

	// deletePolicy: Keep (default) leaves <dataPath>/<containerName> on the
	// server when the cluster is deleted; Delete removes it.
	// +optional
	// +kubebuilder:validation:Enum=Keep;Delete
	// +kubebuilder:default=Keep
	DeletePolicy string `json:"deletePolicy,omitempty"`

	// controlPlaneEndpoint is the API server: the server's host, port 6443.
	// Set by the provider.
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`
}

// UnraidControlPlaneInitializationStatus reports Cluster API initialization.
// +kubebuilder:validation:MinProperties=1
type UnraidControlPlaneInitializationStatus struct {
	// controlPlaneInitialized is true once the API server answers and the
	// kubeconfig Secret exists.
	// +optional
	ControlPlaneInitialized *bool `json:"controlPlaneInitialized,omitempty"`
}

// UnraidControlPlaneStatus is the observed state.
type UnraidControlPlaneStatus struct {
	// +optional
	Initialization UnraidControlPlaneInitializationStatus `json:"initialization,omitempty,omitzero"`
	// version is the Kubernetes version running.
	// +optional
	Version string `json:"version,omitempty"`
	// image is the k3s image the container runs.
	// +optional
	Image string `json:"image,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=unraidcontrolplanes,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.version"
// +kubebuilder:printcolumn:name="Initialized",type="boolean",JSONPath=".status.initialization.controlPlaneInitialized"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// UnraidControlPlane is a k3s server running as a container on an Unraid
// server.
type UnraidControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UnraidControlPlaneSpec   `json:"spec,omitempty"`
	Status UnraidControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnraidControlPlaneList is a list of UnraidControlPlanes.
type UnraidControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnraidControlPlane `json:"items"`
}

// UnraidControlPlaneTemplateResourceSpec is a template's control plane
// spec; version comes from the Cluster's topology.
type UnraidControlPlaneTemplateResourceSpec struct {
	// +optional
	Image string `json:"image,omitempty"`
	// +optional
	ContainerName string `json:"containerName,omitempty"`
	// +optional
	DataPath string `json:"dataPath,omitempty"`
	// +optional
	MountShares *bool `json:"mountShares,omitempty"`
	// +optional
	DockerSocket bool `json:"dockerSocket,omitempty"`
	// +optional
	Disable []string `json:"disable,omitempty"`
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`
	// +optional
	RegistryMirrors map[string][]string `json:"registryMirrors,omitempty"`
	// +optional
	// +kubebuilder:validation:Enum=Keep;Delete
	DeletePolicy string `json:"deletePolicy,omitempty"`
}

// UnraidControlPlaneTemplateResource is a template's UnraidControlPlane.
type UnraidControlPlaneTemplateResource struct {
	// +optional
	ObjectMeta ObjectMeta                             `json:"metadata,omitempty,omitzero"`
	Spec       UnraidControlPlaneTemplateResourceSpec `json:"spec"`
}

// UnraidControlPlaneTemplateSpec is an UnraidControlPlaneTemplate's spec.
type UnraidControlPlaneTemplateSpec struct {
	Template UnraidControlPlaneTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=unraidcontrolplanetemplates,scope=Namespaced,categories=cluster-api

// UnraidControlPlaneTemplate is the ClusterClass template for
// UnraidControlPlanes.
type UnraidControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec UnraidControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// UnraidControlPlaneTemplateList is a list of UnraidControlPlaneTemplates.
type UnraidControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnraidControlPlaneTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnraidControlPlane{}, &UnraidControlPlaneList{}, &UnraidControlPlaneTemplate{}, &UnraidControlPlaneTemplateList{})
}
