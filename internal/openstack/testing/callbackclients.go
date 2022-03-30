package testing

import (
	"context"

	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
)

type CallbackGroupClient struct {
	ListFunc   func(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error)
	CreateFunc func(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error)
	GetFunc    func(ctx context.Context, id string) (*groups.SecGroup, error)
	UpdateFunc func(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackGroupClient) List(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackGroupClient) Create(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackGroupClient) Update(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackGroupClient) Get(ctx context.Context, id string) (*groups.SecGroup, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackGroupClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackRuleClient struct {
	ListFunc   func(ctx context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error)
	CreateFunc func(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error)
	GetFunc    func(ctx context.Context, id string) (*rules.SecGroupRule, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackRuleClient) List(ctx context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackRuleClient) Create(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackRuleClient) Get(ctx context.Context, id string) (*rules.SecGroupRule, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackRuleClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackFipClient struct {
	ListFunc   func(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error)
	CreateFunc func(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error)
	UpdateFunc func(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error)
	GetFunc    func(ctx context.Context, id string) (*floatingips.FloatingIP, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackFipClient) List(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackFipClient) Create(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackFipClient) Update(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackFipClient) Get(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackFipClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackPortClient struct {
	ListFunc   func(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error)
	GetFunc    func(ctx context.Context, id string) (*ports.Port, error)
	CreateFunc func(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error)
	UpdateFunc func(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackPortClient) List(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackPortClient) Get(ctx context.Context, id string) (*ports.Port, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackPortClient) Create(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackPortClient) Update(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackPortClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackServerClient struct {
	ListFunc   func(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error)
	CreateFunc func(ctx context.Context, opts servers.CreateOptsBuilder) (*servers.Server, error)
	GetFunc    func(ctx context.Context, id string) (*servers.Server, error)
	UpdateFunc func(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackServerClient) List(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackServerClient) Create(ctx context.Context, opts servers.CreateOptsBuilder) (*servers.Server, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackServerClient) Get(ctx context.Context, id string) (*servers.Server, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackServerClient) Update(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackServerClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackKeypairClient struct {
	ListFunc   func(ctx context.Context) ([]keypairs.KeyPair, error)
	CreateFunc func(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error)
	GetFunc    func(ctx context.Context, name string) (*keypairs.KeyPair, error)
	DeleteFunc func(ctx context.Context, name string) error
}

func (r *CallbackKeypairClient) List(ctx context.Context) ([]keypairs.KeyPair, error) {
	return r.ListFunc(ctx)
}
func (r *CallbackKeypairClient) Create(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackKeypairClient) Get(ctx context.Context, name string) (*keypairs.KeyPair, error) {
	return r.GetFunc(ctx, name)
}
func (r *CallbackKeypairClient) Delete(ctx context.Context, name string) error {
	return r.DeleteFunc(ctx, name)
}

type CallbackAttachInterfaceClient struct {
	ListFunc   func(ctx context.Context, serverID string) ([]attachinterfaces.Interface, error)
	GetFunc    func(ctx context.Context, serverID, portID string) (*attachinterfaces.Interface, error)
	CreateFunc func(ctx context.Context, serverID string, opts attachinterfaces.CreateOptsBuilder) (*attachinterfaces.Interface, error)
	DeleteFunc func(ctx context.Context, serverID, portID string) error
}

func (r *CallbackAttachInterfaceClient) List(ctx context.Context, serverID string) ([]attachinterfaces.Interface, error) {
	return r.ListFunc(ctx, serverID)
}
func (r *CallbackAttachInterfaceClient) Get(ctx context.Context, serverID, portID string) (*attachinterfaces.Interface, error) {
	return r.GetFunc(ctx, serverID, portID)
}
func (r *CallbackAttachInterfaceClient) Create(
	ctx context.Context,
	serverID string,
	opts attachinterfaces.CreateOptsBuilder,
) (*attachinterfaces.Interface, error) {
	return r.CreateFunc(ctx, serverID, opts)
}
func (r *CallbackAttachInterfaceClient) Delete(ctx context.Context, serverID, portID string) error {
	return r.DeleteFunc(ctx, serverID, portID)
}

type CallbackLoadBalancerClient struct {
	ListFunc        func(ctx context.Context, opts loadbalancers.ListOptsBuilder) ([]loadbalancers.LoadBalancer, error)
	CreateFunc      func(ctx context.Context, opts loadbalancers.CreateOptsBuilder) (*loadbalancers.LoadBalancer, error)
	GetFunc         func(ctx context.Context, id string) (*loadbalancers.LoadBalancer, error)
	UpdateFunc      func(ctx context.Context, id string, opts loadbalancers.UpdateOpts) (*loadbalancers.LoadBalancer, error)
	DeleteFunc      func(ctx context.Context, id string, opts loadbalancers.DeleteOptsBuilder) error
	GetStatusesFunc func(ctx context.Context, id string) (*loadbalancers.StatusTree, error)
	GetStatsFunc    func(ctx context.Context, id string) (*loadbalancers.Stats, error)
	FailoverFunc    func(ctx context.Context, id string) error
}

func (r CallbackLoadBalancerClient) List(ctx context.Context, opts loadbalancers.ListOptsBuilder) ([]loadbalancers.LoadBalancer, error) {
	return r.ListFunc(ctx, opts)
}

func (r CallbackLoadBalancerClient) Create(ctx context.Context, opts loadbalancers.CreateOptsBuilder) (*loadbalancers.LoadBalancer, error) {
	return r.CreateFunc(ctx, opts)
}

func (r CallbackLoadBalancerClient) Get(ctx context.Context, id string) (*loadbalancers.LoadBalancer, error) {
	return r.GetFunc(ctx, id)
}

func (r CallbackLoadBalancerClient) Update(
	ctx context.Context,
	id string,
	opts loadbalancers.UpdateOpts,
) (*loadbalancers.LoadBalancer, error) {
	return r.UpdateFunc(ctx, id, opts)
}

func (r CallbackLoadBalancerClient) Delete(ctx context.Context, id string, opts loadbalancers.DeleteOptsBuilder) error {
	return r.DeleteFunc(ctx, id, opts)
}

func (r CallbackLoadBalancerClient) GetStatuses(ctx context.Context, id string) (*loadbalancers.StatusTree, error) {
	return r.GetStatusesFunc(ctx, id)
}

func (r CallbackLoadBalancerClient) GetStats(ctx context.Context, id string) (*loadbalancers.Stats, error) {
	return r.GetStatsFunc(ctx, id)
}

func (r CallbackLoadBalancerClient) Failover(ctx context.Context, id string) error {
	return r.FailoverFunc(ctx, id)
}
