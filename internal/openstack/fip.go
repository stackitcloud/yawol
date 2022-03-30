package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
)

// The OSFloatingIPClient is a implementation for FipClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSFloatingIPClient struct {
	networkV2 *gophercloud.ServiceClient
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSFloatingIPClient) Configure(networkV2 *gophercloud.ServiceClient) *OSFloatingIPClient {
	r.networkV2 = networkV2
	return r
}

// Invokes floatingips.List() in gophercloud's floating ip package and extracts all floating ips.
// Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) List(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error) {
	r.networkV2.Context = ctx
	pages, err := floatingips.List(r.networkV2, opts).AllPages()
	r.networkV2.Context = nil
	if err != nil {
		return nil, err
	}
	return floatingips.ExtractFloatingIPs(pages)
}

// Invokes floatingips.Create() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Create(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error) {
	r.networkV2.Context = ctx
	fip, err := floatingips.Create(r.networkV2, opts).Extract()
	r.networkV2.Context = nil
	return fip, err
}

// Invokes floatingips.Update() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Update(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error) {
	r.networkV2.Context = ctx
	fip, err := floatingips.Update(r.networkV2, id, opts).Extract()
	r.networkV2.Context = nil
	return fip, err
}

// Invokes floatingips.Get() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Get(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
	r.networkV2.Context = ctx
	fip, err := floatingips.Get(r.networkV2, id).Extract()
	r.networkV2.Context = nil
	return fip, err
}

// Invokes floatingips.Delete() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Delete(ctx context.Context, id string) error {
	r.networkV2.Context = ctx
	err := floatingips.Delete(r.networkV2, id).ExtractErr()
	r.networkV2.Context = nil
	return err
}
