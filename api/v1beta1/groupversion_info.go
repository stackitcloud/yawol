// Package v1beta1 contains API Schema definitions for the yawol v1beta1 API group
// +kubebuilder:object:generate=true
// +kubebuilder:validation:Required
// +groupName=yawol.stackit.cloud
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "yawol.stackit.cloud", Version: "v1beta1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// addKnownTypes registers the types of this group-version with the given scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&LoadBalancer{},
		&LoadBalancerList{},
		&LoadBalancerMachine{},
		&LoadBalancerMachineList{},
		&LoadBalancerSet{},
		&LoadBalancerSetList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
