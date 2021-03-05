package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// LoadBalancerMachine is the Schema for the LoadBalancerMachine's API.
type LoadBalancerMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LoadBalancerMachineSpec   `json:"spec,omitempty"`
	Status LoadBalancerMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// LoadBalancerMachineList contains a list of LoadBalancerMachine
type LoadBalancerMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoadBalancerMachine `json:"items"`
}

// LoadBalancerMachineSpec defines the desired state of LoadBalancerMachine
type LoadBalancerMachineSpec struct {
	// Infrastructure defines parameters for the Infrastructure.
	Infrastructure LoadBalancerInfrastructure `json:"infrastructure"`
	// FloatingID defines the  openstack ID from the FloatingIP.
	FloatingID string `json:"floatingID"`
	// PortID defines the openstack ID from the Port.
	PortID string `json:"portID"`
	// LoadBalancerRef defines a reference to the LoadBalancer Object.
	LoadBalancerRef LoadBalancerRef `json:"loadBalancerRef"`
}

// LoadBalancerMachineTemplateSpec defines the desired state of LoadBalancerSet.
type LoadBalancerMachineTemplateSpec struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is the spec for the LoadBalancerMachine.
	Spec LoadBalancerMachineSpec `json:"spec"`
}

// LoadBalancerMachineStatus defines the observed state of LoadBalancerMachine.
type LoadBalancerMachineStatus struct {
	// Conditions contains condition information for a LoadBalancerMachine.
	// +optional
	Conditions *[]corev1.NodeCondition `json:"conditions,omitempty"`
	// Metrics contains metrics for a LoadBalancerMachine.
	// +optional
	Metrics *[]LoadBalancerMachineMetric `json:"metrics,omitempty"`
	// CreationTimestamp contains the creation timestamp a LoadBalancerMachine.
	// +optional
	CreationTimestamp *metav1.Timestamp `json:"creationTimestamp,omitempty"`
	// ServerID contains the openstack server ID for a LoadBalancerMachine.
	// +optional
	ServerID *string `json:"serverID,omitempty"`
}

// LoadBalancerMachineMetric describes a metric of the LoadBalancerMachine
type LoadBalancerMachineMetric struct {
	// Type is the type of a metric
	Type string `json:"type"`
	// Value is the value of a metric
	Value string `json:"value"`
	// Time is the timestamp if the metric
	Time metav1.Time `json:"timestamp"`
}

func init() {
	SchemeBuilder.Register(&LoadBalancerMachine{}, &LoadBalancerMachineList{})
}
