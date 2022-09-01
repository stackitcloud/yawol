package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
)

// The OSServerClient is a implementation for ServerClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSServerClient struct {
	computeV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes ComputeV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSServerClient) Configure(
	networkClient *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSServerClient {
	r.computeV2 = networkClient
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// Invokes servers.List() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) List(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	page, err := servers.List(r.computeV2, opts).AllPages()
	if err != nil {
		return nil, err
	}
	return servers.ExtractServers(page)
}

// Invokes servers.Create() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Create(ctx context.Context, opts servers.CreateOptsBuilder) (*servers.Server, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	srv, err := servers.Create(r.computeV2, opts).Extract()
	r.computeV2.Context = nil
	return srv, err
}

// Invokes servers.Get() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Get(ctx context.Context, id string) (*servers.Server, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	srv, err := servers.Get(r.computeV2, id).Extract()
	return srv, err
}

// Invokes servers.Update() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Update(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationUpdate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	srv, err := servers.Update(r.computeV2, id, opts).Extract()
	return srv, err
}

// Invokes servers.Delete() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	err := servers.Delete(r.computeV2, id).ExtractErr()
	return err
}
