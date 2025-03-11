package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TenantSpec   `json:"spec,omitempty"`
	Status            TenantStatus `json:"status,omitempty"`
}

// TenantSpec describes a Tenant's desired state.
type TenantSpec struct {
	// TODO: Add fields here that describe a Tenant's desired state.

	// AdditionalLabels describes new key-value pairs that user wants to add to namespace labels.
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`
	// AllowEgress is a bool value that controls if pods may send traffic outside the cluster or not.
	AllowEgress bool `json:"allowEgress,omitempty"`
}

// TenantStatus describes a Tenant's current status.
type TenantStatus struct {
	// TODO: Add fields here that describe a Tenant's current state.

	// ErrorMessage describes if the namespace exists and is owned by a previous Tenant or not.
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// +kubebuilder:object:root=true

// TenantList is a list of Tenant resources.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tenant `json:"items,omitempty"`
}
