package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
)

// The OSKeypairClient is a implementation for KeyPairClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSKeypairClient struct {
	computeV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes ComputeV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSKeypairClient) Configure(
	computeV2 *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSKeypairClient {
	r.computeV2 = computeV2
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// Invokes keypairs.List() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) List(ctx context.Context) ([]keypairs.KeyPair, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectKeyPair, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	page, err := keypairs.List(r.computeV2).AllPages()
	if err != nil {
		return nil, err
	}
	return keypairs.ExtractKeyPairs(page)
}

// Invokes keypairs.Create() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) Create(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectKeyPair, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	kp, err := keypairs.Create(r.computeV2, opts).Extract()
	return kp, err
}

// Invokes keypairs.Get() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) Get(ctx context.Context, name string) (*keypairs.KeyPair, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectKeyPair, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	kp, err := keypairs.Get(r.computeV2, name).Extract()
	return kp, err
}

// Invokes keypairs.Delete() in gophercloud's keypairs package. Uses the computeV2 client provided in Configure().
func (r *OSKeypairClient) Delete(ctx context.Context, name string) error {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectKeyPair, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	err := keypairs.Delete(r.computeV2, name).ExtractErr()
	return err
}
