package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
)

// The OSAttachInterfacesClient is a implementation for AttachInterfaceClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSAttachInterfacesClient struct {
	computeV2 *gophercloud.ServiceClient
}

// Configure takes ComputeV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSAttachInterfacesClient) Configure(computeV2 *gophercloud.ServiceClient) *OSAttachInterfacesClient {
	r.computeV2 = computeV2
	return r
}

// Invokes attachinterfaces.List() in gophercloud's attachinterfaces package. Uses the computeV2 client provided in Configure().
func (r *OSAttachInterfacesClient) List(ctx context.Context, serverID string) ([]attachinterfaces.Interface, error) {
	r.computeV2.Context = ctx
	page, err := attachinterfaces.List(r.computeV2, serverID).AllPages()
	r.computeV2.Context = nil
	if err != nil {
		return nil, err
	}
	return attachinterfaces.ExtractInterfaces(page)
}

// Invokes attachinterfaces.Get() in gophercloud's attachinterfaces package. Uses the computeV2 client provided in Configure().
func (r *OSAttachInterfacesClient) Get(ctx context.Context, serverID, portID string) (*attachinterfaces.Interface, error) {
	r.computeV2.Context = ctx
	i, err := attachinterfaces.Get(r.computeV2, serverID, portID).Extract()
	r.computeV2.Context = nil
	return i, err
}

// Invokes attachinterfaces.Create() in gophercloud's attachinterfaces package. Uses the computeV2 client provided in Configure().
func (r *OSAttachInterfacesClient) Create(ctx context.Context,
	serverID string,
	opts attachinterfaces.CreateOptsBuilder,
) (*attachinterfaces.Interface, error) {
	r.computeV2.Context = ctx
	i, err := attachinterfaces.Create(r.computeV2, serverID, opts).Extract()
	r.computeV2.Context = nil
	return i, err
}

// Invokes attachinterfaces.Delete() in gophercloud's attachinterfaces package. Uses the computeV2 client provided in Configure().
func (r *OSAttachInterfacesClient) Delete(ctx context.Context, serverID, portID string) error {
	r.computeV2.Context = ctx
	err := attachinterfaces.Delete(r.computeV2, serverID, portID).ExtractErr()
	r.computeV2.Context = nil
	return err
}
