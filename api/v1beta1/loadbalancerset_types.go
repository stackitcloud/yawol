package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=lbs
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas
// LoadBalancerSet is the Schema for the LoadBalancerSet's API.
type LoadBalancerSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoadBalancerSetSpec   `json:"spec,omitempty"`
	Status LoadBalancerSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// LoadBalancerSetList contains a list of LoadBalancerSet.
type LoadBalancerSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoadBalancerSet `json:"items"`
}

// LoadBalancerSetSpec defines the desired state of LoadBalancerSet.
type LoadBalancerSetSpec struct {
	// Selector is a label query over pods that should match the replica count.
	Selector metav1.LabelSelector `json:"selector"`
	// Replicas defines the number of LoadBalancer that should run. Defaults to 1.
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=0
	// +optional
	Replicas int `json:"replicas,omitempty"`
	// Template defines a template for the LoadBalancerMachine. This is used to instantiate LoadBalancerMachine.
	Template LoadBalancerMachineTemplateSpec `json:"template"`
}

// LoadBalancerSetStatus defines the observed state of LoadBalancerSet.
type LoadBalancerSetStatus struct {
	// AvailableReplicas are the current running replicas.
	// +optional
	AvailableReplicas *int `json:"availableReplicas,omitempty"`
	// AvailableReplicas are the current running replicas.
	// +kubebuilder:default:="fafa"
	Test string `json:"test,omitempty"`
	// ReadyReplicas are the current ready replicas.
	// +optional
	ReadyReplicas *int `json:"readyReplicas,omitempty"`
	// Replicas are the desired replicas.
	// +optional
	Replicas *int `json:"replicas,omitempty"`
}

func init() {
	SchemeBuilder.Register(&LoadBalancerSet{}, &LoadBalancerSetList{})
}
