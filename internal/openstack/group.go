package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
)

// The OSGroupClient is a implementation for GroupClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSGroupClient struct {
	networkV2 *gophercloud.ServiceClient
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSGroupClient) Configure(networkClient *gophercloud.ServiceClient) *OSGroupClient {
	r.networkV2 = networkClient
	return r
}

// Invokes groups.List() in gophercloud's groups package and extracts all security groups.
// Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) List(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
	r.networkV2.Context = ctx
	pages, err := groups.List(r.networkV2, opts).AllPages()
	r.networkV2.Context = nil
	if err != nil {
		return nil, err
	}
	return groups.ExtractGroups(pages)
}

// Invokes groups.Create() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Create(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error) {
	r.networkV2.Context = ctx
	group, err := groups.Create(r.networkV2, opts).Extract()
	r.networkV2.Context = nil
	return group, err
}

// Invokes groups.Update() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Update(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error) {
	r.networkV2.Context = ctx
	group, err := groups.Update(r.networkV2, id, opts).Extract()
	r.networkV2.Context = nil
	return group, err
}

// Invokes groups.Get() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Get(ctx context.Context, id string) (*groups.SecGroup, error) {
	r.networkV2.Context = ctx
	group, err := groups.Get(r.networkV2, id).Extract()
	r.networkV2.Context = nil
	return group, err
}

// Invokes groups.Delete() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Delete(ctx context.Context, id string) error {
	r.networkV2.Context = ctx
	err := groups.Delete(r.networkV2, id).ExtractErr()
	r.networkV2.Context = nil
	return err
}
