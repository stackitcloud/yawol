/*
The openstack package provides you an incomplete interface to access openstack resource via gophercloud's openstack functions.
All CRUD calls on the interface will invoke gophercloud functions.

To use this package, create OSServerClient an initialize it with the Client.Configure method.

	osClient := openstack.OSClient{}
	err := osClient.Configure(iniData)

If you want to mock the interface, you can use testing.MockClient and the implementations
of callback clients (e.g. testing.CallbackRuleClient)

	mockClient := testing.MockClient{}
	mockClient.StoredValues = map[string]interface{}{}
	mockClient.GroupClientObj = &testing.CallbackGroupClient{
		ListFunc: func(opts groups.ListOpts) ([]groups.SecGroup, error) {
			return []groups.SecGroup{
				{
					Name:  "sec-group-name",
					ID:    "sec-group-id",
					Rules: desiredRules,
				},
			}, nil
		},
	}
*/
package openstack

import (
	"context"
	"time"

	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
)

// Client provides a interface to configure and use different OpenStack clients.
type Client interface {
	// Takes the content of an ini-file, to configure the openstack client
	Configure(ini []byte, timeout time.Duration) error
	// Returns the FipClient created from the configured ini
	FipClient(ctx context.Context) (FipClient, error)
	// Returns the PortClient created from the configured ini
	PortClient(ctx context.Context) (PortClient, error)
	// Returns the GroupClient created from the configured ini
	GroupClient(ctx context.Context) (GroupClient, error)
	// Returns the RuleClient created from the configured ini
	RuleClient(ctx context.Context) (RuleClient, error)
	// Returns the ServerClient created from the configured ini
	ServerClient(ctx context.Context) (ServerClient, error)
	// Returns the KeyPairClient created from the configured ini
	KeyPairClient(ctx context.Context) (KeyPairClient, error)
	// Returns the AttachInterfaceClient created from the configured ini
	AttachInterfaceClient(ctx context.Context) (AttachInterfaceClient, error)
	// Returns the AttachInterfaceClient created from the configured ini
	LoadBalancerClient(ctx context.Context) (LoadBalancerClient, error)
}

// FipClient is used to modify FloatingIPs in an OpenStack environment.
// It provides methods with CRUD functionalities
type FipClient interface {
	List(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error)
	Create(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error)
	Update(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error)
	Get(ctx context.Context, id string) (*floatingips.FloatingIP, error)
	Delete(ctx context.Context, id string) error
}

// PortClient is used to modify Network Ports in an OpenStack environment.
// It provides methods with CRUD functionalities.
type PortClient interface {
	List(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error)
	Get(ctx context.Context, id string) (*ports.Port, error)
	Create(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error)
	Update(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error)
	Delete(ctx context.Context, id string) error
}

// GroupClient is used to modify Network Security Groups in an OpenStack environment.
// It provides methods with CRUD functionalities.
type GroupClient interface {
	List(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error)
	Create(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error)
	Update(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error)
	Get(ctx context.Context, id string) (*groups.SecGroup, error)
	Delete(ctx context.Context, id string) error
}

// RuleClient is used to modify Network Security Rules in an OpenStack environment.
// Rules must be created in the context of a Security Group, which can be created with the GroupClient.
// It provides methods with CRUD functionalities.
type RuleClient interface {
	List(ctx context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error)
	Create(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error)
	Get(ctx context.Context, id string) (*rules.SecGroupRule, error)
	Delete(ctx context.Context, id string) error
}

// ServerClient is used to modify Virtual Machines in an OpenStack environment.
// It provides methods with CRUD functionalities.
type ServerClient interface {
	List(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error)
	Create(ctx context.Context, opts servers.CreateOptsBuilder) (*servers.Server, error)
	Get(ctx context.Context, id string) (*servers.Server, error)
	Update(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error)
	Delete(ctx context.Context, id string) error
}

// KeyPairClient is used to create and delete ssh keys in an OpenStack environment.
// It provides methods with CRD functionalities.
type KeyPairClient interface {
	List(ctx context.Context) ([]keypairs.KeyPair, error)
	Create(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error)
	Get(ctx context.Context, name string) (*keypairs.KeyPair, error)
	Delete(ctx context.Context, name string) error
}

// AttachInterfaceClient is used to attach and detach network ports to a server in an OpenStack environment.
// They can be created with ServerClient and PortClient.
// It provides methods with CRD functionalities.
type AttachInterfaceClient interface {
	List(ctx context.Context, serverID string) ([]attachinterfaces.Interface, error)
	Get(ctx context.Context, serverID, portID string) (*attachinterfaces.Interface, error)
	Create(ctx context.Context, serverID string, opts attachinterfaces.CreateOptsBuilder) (*attachinterfaces.Interface, error)
	Delete(ctx context.Context, serverID, portID string) error
}

type LoadBalancerClient interface {
	List(ctx context.Context, opts loadbalancers.ListOptsBuilder) ([]loadbalancers.LoadBalancer, error)
	Create(ctx context.Context, opts loadbalancers.CreateOptsBuilder) (*loadbalancers.LoadBalancer, error)
	Get(ctx context.Context, id string) (*loadbalancers.LoadBalancer, error)
	Update(ctx context.Context, id string, opts loadbalancers.UpdateOpts) (*loadbalancers.LoadBalancer, error)
	Delete(ctx context.Context, id string, opts loadbalancers.DeleteOptsBuilder) error
	GetStatuses(ctx context.Context, id string) (*loadbalancers.StatusTree, error)
	GetStats(ctx context.Context, id string) (*loadbalancers.Stats, error)
	Failover(ctx context.Context, id string) error
}
