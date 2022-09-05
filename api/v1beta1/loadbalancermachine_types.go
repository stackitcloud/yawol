package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=lbm
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="EnvoyUpToDate",type=string,JSONPath=`.status.conditions[?(@.type=="EnvoyUpToDate")].status`
// +kubebuilder:printcolumn:name="KeepalivedMaster",type=string,JSONPath=`.status.conditions[?(@.type=="KeepalivedMaster")].status`
// +kubebuilder:printcolumn:name="Load1",type=string,JSONPath=`.status.metrics[?(@.type=="load1")].value`
// +kubebuilder:printcolumn:name="creationTimestamp",type=string,JSONPath=`.status.creationTimestamp`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`

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
	// PortID defines the  openstack ID of the port attached to the FloatingIP.
	PortID string `json:"portID"`
	// LoadBalancerRef defines a reference to the LoadBalancer Object.
	LoadBalancerRef LoadBalancerRef `json:"loadBalancerRef"`
}

// LoadBalancerMachineTemplateSpec defines the desired state of LoadBalancerSet.
type LoadBalancerMachineTemplateSpec struct {
	// Labels for the LoadBalancerMachine
	Labels map[string]string `json:"labels"`
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
	CreationTimestamp *metav1.Time `json:"creationTimestamp,omitempty"`
	// LastOpenstackReconcile contains the timestamp of the last openstack reconciliation.
	// +optional
	LastOpenstackReconcile *metav1.Time `json:"lastOpenstackReconcile,omitempty"`
	// ServerID contains the openstack server ID for a LoadBalancerMachine.
	// +optional
	ServerID *string `json:"serverID,omitempty"`
	// PortID contains the openstack port ID for a LoadBalancerMachine.
	// +optional
	PortID *string `json:"portID,omitempty"`
	// ServiceAccountName contains the namespacedName from the ServiceAccount for a LoadBalancerMachine.
	// +optional
	ServiceAccountName *string `json:"serviceAccountName,omitempty"`
	// RoleName contains the namespacedName from the Role for a LoadBalancerMachine.
	// +optional
	RoleName *string `json:"roleName,omitempty"`
	// RoleBindingName contains the namespacedName from the RoleBinding for a LoadBalancerMachine.
	// +optional
	RoleBindingName *string `json:"roleBindingName,omitempty"`
}

// LoadBalancerMachineMetric describes a metric of the LoadBalancerMachine
type LoadBalancerMachineMetric struct {
	// Type is the type of the metric
	Type string `json:"type"`
	// Value is the value of a metric
	Value string `json:"value"`
	// Time is the timestamp if the metric
	Time metav1.Time `json:"timestamp"`
}

func init() {
	SchemeBuilder.Register(&LoadBalancerMachine{}, &LoadBalancerMachineList{})
}
