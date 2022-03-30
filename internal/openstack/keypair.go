package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
)

// The OSKeypairClient is a implementation for KeyPairClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSKeypairClient struct {
	computeV2 *gophercloud.ServiceClient
}

// Configure takes ComputeV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSKeypairClient) Configure(computeV2 *gophercloud.ServiceClient) *OSKeypairClient {
	r.computeV2 = computeV2
	return r
}

// Invokes keypairs.List() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) List(ctx context.Context) ([]keypairs.KeyPair, error) {
	r.computeV2.Context = ctx
	page, err := keypairs.List(r.computeV2).AllPages()
	r.computeV2.Context = nil
	if err != nil {
		return nil, err
	}
	return keypairs.ExtractKeyPairs(page)
}

// Invokes keypairs.Create() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) Create(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error) {
	r.computeV2.Context = ctx
	kp, err := keypairs.Create(r.computeV2, opts).Extract()
	r.computeV2.Context = nil
	return kp, err
}

// Invokes keypairs.Get() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) Get(ctx context.Context, name string) (*keypairs.KeyPair, error) {
	r.computeV2.Context = ctx
	kp, err := keypairs.Get(r.computeV2, name).Extract()
	r.computeV2.Context = nil
	return kp, err
}

// Invokes keypairs.Delete() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) Delete(ctx context.Context, name string) error {
	r.computeV2.Context = ctx
	err := keypairs.Delete(r.computeV2, name).ExtractErr()
	r.computeV2.Context = nil
	return err
}
