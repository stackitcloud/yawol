package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
)

// The OSLoadBalancerClient is a implementation for LoadBalancerClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSLoadBalancerClient struct {
	loadBalancerV2 *gophercloud.ServiceClient
}

// Configure takes LoadBalancerV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSLoadBalancerClient) Configure(loadBalancerClient *gophercloud.ServiceClient) *OSLoadBalancerClient {
	r.loadBalancerV2 = loadBalancerClient
	return r
}

// Invokes loadbalancers.List() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) List(ctx context.Context, opts loadbalancers.ListOptsBuilder) ([]loadbalancers.LoadBalancer, error) {
	r.loadBalancerV2.Context = ctx
	page, err := loadbalancers.List(r.loadBalancerV2, opts).AllPages()
	r.loadBalancerV2.Context = nil
	if err != nil {
		return nil, err
	}
	return loadbalancers.ExtractLoadBalancers(page)
}

// Invokes loadbalancers.Create() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) Create(ctx context.Context, opts loadbalancers.CreateOptsBuilder) (*loadbalancers.LoadBalancer, error) {
	r.loadBalancerV2.Context = ctx
	lb, err := loadbalancers.Create(r.loadBalancerV2, opts).Extract()
	r.loadBalancerV2.Context = nil
	return lb, err
}

// Invokes loadbalancers.Get() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) Get(ctx context.Context, id string) (*loadbalancers.LoadBalancer, error) {
	r.loadBalancerV2.Context = ctx
	lb, err := loadbalancers.Get(r.loadBalancerV2, id).Extract()
	r.loadBalancerV2.Context = nil
	return lb, err
}

// Invokes loadbalancers.Update() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) Update(ctx context.Context, id string, opts loadbalancers.UpdateOpts) (*loadbalancers.LoadBalancer, error) {
	r.loadBalancerV2.Context = ctx
	lb, err := loadbalancers.Update(r.loadBalancerV2, id, opts).Extract()
	r.loadBalancerV2.Context = nil
	return lb, err
}

// Invokes loadbalancers.Delete() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) Delete(ctx context.Context, id string, opts loadbalancers.DeleteOptsBuilder) error {
	r.loadBalancerV2.Context = ctx
	err := loadbalancers.Delete(r.loadBalancerV2, id, opts).ExtractErr()
	r.loadBalancerV2.Context = nil
	return err
}

// Invokes loadbalancers.GetStatuses() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) GetStatuses(ctx context.Context, id string) (*loadbalancers.StatusTree, error) {
	r.loadBalancerV2.Context = ctx
	lb, err := loadbalancers.GetStatuses(r.loadBalancerV2, id).Extract()
	r.loadBalancerV2.Context = nil
	return lb, err
}

// Invokes loadbalancers.GetStats() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) GetStats(ctx context.Context, id string) (*loadbalancers.Stats, error) {
	r.loadBalancerV2.Context = ctx
	lb, err := loadbalancers.GetStats(r.loadBalancerV2, id).Extract()
	r.loadBalancerV2.Context = nil
	return lb, err
}

// Invokes loadbalancers.Failover() in gophercloud's loadbalancer package. Uses the LoadBalancerV2 client provided in Configure().
func (r *OSLoadBalancerClient) Failover(ctx context.Context, id string) error {
	r.loadBalancerV2.Context = ctx
	err := loadbalancers.Failover(r.loadBalancerV2, id).ExtractErr()
	r.loadBalancerV2.Context = nil
	return err
}
