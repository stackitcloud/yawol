package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=lb
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas
// +kubebuilder:printcolumn:name="DESIRED",type=string,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="CURRENT",type=string,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="READY",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="externalIP",type=string,JSONPath=`.status.externalIP`

// LoadBalancer is the Schema for the YAWOL LoadBalancer API
type LoadBalancer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   LoadBalancerSpec   `json:"spec,omitempty"`
	Status LoadBalancerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LoadBalancerList contains a list of LoadBalancer.
type LoadBalancerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LoadBalancer `json:"items"`
}

// LoadBalancerSpec defines the desired state of LoadBalancer
type LoadBalancerSpec struct {
	// This label selector matches the load balancer sets deriving from the load balancer
	Selector metav1.LabelSelector `json:"selector"`
	// Replicas defines the number of LoadBalancers that should run.
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=0
	// +optional
	Replicas int `json:"replicas,omitempty"`
	// ExternalIP is a externalIP (FIP or private) that should be used for a new LoadBalancer
	// +optional
	ExternalIP *string `json:"externalIP,omitempty"`
	// InternalLB is a bool for internal LoadBalancer. If set to false a FloatingIP will be assigned to the LB. Defaults to false.
	// +kubebuilder:default:=false
	// +optional
	InternalLB bool `json:"internalLB,omitempty"`
	// Endpoints defines the Endpoints for the LoadBalancer.
	Endpoints []LoadBalancerEndpoint `json:"endpoints,omitempty"`
	// Ports defines the Ports for the LoadBalancer (copy from service)
	Ports []corev1.ServicePort `json:"ports,omitempty"`
	// Infrastructure defines parameters for the Infrastructure
	Infrastructure LoadBalancerInfrastructure `json:"infrastructure"`
}

// LoadBalancerEndpoint defines a Endpoint for the LoadBalancer
type LoadBalancerEndpoint struct {
	// Name defines a name for the Endpoint (example: node name).
	Name string `json:"name"`
	// Addresses is a list of addresses for the endpoint, they can contain IPv4 and IPv6 addresses.
	Addresses []string `json:"addresses,omitempty"`
}

// LoadBalancerInfrastructure defines a Endpoint for the LoadBalancer
type LoadBalancerInfrastructure struct {
	// FloatingNetID defines a openstack ID for the floatingNet.
	// +optional
	FloatingNetID *string `json:"floatingNetID,omitempty"`
	// NetworkID defines a openstack ID for the network.
	NetworkID string `json:"networkID"`
	// Flavor defines openstack flavor for the LoadBalancer. Uses a default if not defined.
	// +optional
	Flavor *OpenstackFlavorRef `json:"flavor,omitempty"`
	// Image defines openstack image for the LoadBalancer. Uses a default if not defined.
	// +optional
	Image *OpenstackImageRef `json:"image,omitempty"`
	// AuthSecretRef defines a secretRef for the openstack secret.
	AuthSecretRef corev1.SecretReference `json:"authSecretRef"`
}

// OpenstackImageRef defines a reference to a Openstack image.
type OpenstackImageRef struct {
	// ImageID is the image ID used for requesting virtual machines.
	// +optional
	ImageID *string `json:"image_id,omitempty"`
	// ImageName is the name of the image used for requesting virtual machines.
	// ImageName is only used if ImageID is not defined.
	// +optional
	ImageName *string `json:"image_name,omitempty"`
	// ImageSearch is a search string to find the image used for requesting virtual machines.
	// Search will be performed in metadata of the images.
	// ImageSearch is only used if ImageName and ImageID are not defined.
	// +optional
	ImageSearch *string `json:"image_search,omitempty"`
}

// OpenstackFlavorRef defines a reference to a Openstack flavor.
// The Flavor defines the amount of cpu cores and memory as well as the size of the root disk.
type OpenstackFlavorRef struct {
	// FlavorID is the flavor ID used for requesting virtual machines.
	// +optional
	FlavorID *string `json:"flavor_id,omitempty"`
	// FlavorName is the name of the flavor used for requesting virtual machines.
	// FlavorName is only used if FlavorID is not defined.
	// +optional
	FlavorName *string `json:"flavor_name,omitempty"`
	// FlavorSearch is a search string to find the flavor used for requesting virtual machines.
	// Search will be performed in metadata of the flavors.
	// FlavorSearch is only used if FlavorName and FlavorID are not defined.
	// +optional
	FlavorSearch *string `json:"flavor_search,omitempty"`
}

// LoadBalancerRef defines a reference to a LoadBalancer object.
type LoadBalancerRef struct {
	// Name is unique within a namespace to reference a LoadBalancer resource.
	Name string `json:"name"`
	// Namespace defines the space within which the LoadBalancer name must be unique.
	Namespace string `json:"namespace"`
}

// LoadBalancerStatus defines the observed state of LoadBalancer.
type LoadBalancerStatus struct {
	// ReadyReplicas are the current running replicas.
	// +optional
	ReadyReplicas *int `json:"readyReplicas,omitempty"`
	// Replicas displays the running lb replicas under this deployment
	// +optional
	Replicas *int `json:"replicas,omitempty"`
	// ExternalIP is the current externalIP (FIP or private). If not defined, no ExternalIP is bound yet.
	// +optional
	ExternalIP *string `json:"externalIP,omitempty"`
	// FloatingID is the current openstack ID from the FloatingIP.
	// +optional
	FloatingID *string `json:"floatingID,omitempty"`
	// FloatingName is the current openstack name from the FloatingIP.
	// +optional
	FloatingName *string `json:"floatingName,omitempty"`
	// PortID is the current openstack ID from the virtual Port.
	// +optional
	PortID *string `json:"portID,omitempty"`
	// PortName is the current openstack name from the virtual Port.
	// +optional
	PortName *string `json:"portName,omitempty"`
	// SecurityGroupID is the current security group ID mapped to the port
	// +optional
	SecurityGroupID *string `json:"security_group_id,omitempty"`
	// SecurityGroupName is the current security group name mapped to the port
	// +optional
	SecurityGroupName *string `json:"security_group_name,omitempty"`
	// NodeRole is the current role metadata that is used by the LB machine's ServiceAccounts.
	// +optional
	NodeRoleRef *rbacv1.RoleRef `json:"nodeRoleRef,omitempty"`
}

func init() {
	SchemeBuilder.Register(&LoadBalancer{}, &LoadBalancerList{})
}
