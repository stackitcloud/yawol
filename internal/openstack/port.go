package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
)

// The OSPortClient is a implementation for PortClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSPortClient struct {
	networkV2 *gophercloud.ServiceClient
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSPortClient) Configure(networkClient *gophercloud.ServiceClient) *OSPortClient {
	r.networkV2 = networkClient
	return r
}

// Invokes ports.List() in gophercloud's ports package and extracts all ports.
// Uses the networkV2 client provided in Configure().
func (r *OSPortClient) List(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error) {
	r.networkV2.Context = ctx
	page, err := ports.List(r.networkV2, opts).AllPages()
	r.networkV2.Context = nil
	if err != nil {
		return nil, err
	}
	return ports.ExtractPorts(page)
}

// Invokes groups.Get() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Get(ctx context.Context, id string) (*ports.Port, error) {
	r.networkV2.Context = ctx
	port, err := ports.Get(r.networkV2, id).Extract()
	r.networkV2.Context = nil
	return port, err
}

// Invokes groups.Create() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Create(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error) {
	r.networkV2.Context = ctx
	port, err := ports.Create(r.networkV2, opts).Extract()
	r.networkV2.Context = nil
	return port, err
}

// Invokes groups.Update() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Update(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error) {
	r.networkV2.Context = ctx
	port, err := ports.Update(r.networkV2, id, opts).Extract()
	r.networkV2.Context = nil
	return port, err
}

// Invokes groups.Delete() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Delete(ctx context.Context, id string) error {
	r.networkV2.Context = ctx
	err := ports.Delete(r.networkV2, id).ExtractErr()
	r.networkV2.Context = nil
	return err
}
