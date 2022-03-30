package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
)

// The OSServerClient is a implementation for ServerClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSServerClient struct {
	computeV2 *gophercloud.ServiceClient
}

// Configure takes ComputeV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSServerClient) Configure(networkClient *gophercloud.ServiceClient) *OSServerClient {
	r.computeV2 = networkClient
	return r
}

// Invokes servers.List() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) List(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error) {
	r.computeV2.Context = ctx
	page, err := servers.List(r.computeV2, opts).AllPages()
	r.computeV2.Context = nil
	if err != nil {
		return nil, err
	}
	return servers.ExtractServers(page)
}

// Invokes servers.Create() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Create(ctx context.Context, opts servers.CreateOptsBuilder) (*servers.Server, error) {
	r.computeV2.Context = ctx
	srv, err := servers.Create(r.computeV2, opts).Extract()
	r.computeV2.Context = nil
	return srv, err
}

// Invokes servers.Get() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Get(ctx context.Context, id string) (*servers.Server, error) {
	r.computeV2.Context = ctx
	srv, err := servers.Get(r.computeV2, id).Extract()
	r.computeV2.Context = nil
	return srv, err
}

// Invokes servers.Update() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Update(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error) {
	r.computeV2.Context = ctx
	srv, err := servers.Update(r.computeV2, id, opts).Extract()
	r.computeV2.Context = nil
	return srv, err
}

// Invokes servers.Delete() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Delete(ctx context.Context, id string) error {
	r.computeV2.Context = ctx
	err := servers.Delete(r.computeV2, id).ExtractErr()
	r.computeV2.Context = nil
	return err
}
