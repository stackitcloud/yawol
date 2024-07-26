package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servergroups"
)

// The OSServerGroupClient is a implementation for ServerGroupClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSServerGroupClient struct {
	computeV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes ComputeV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSServerGroupClient) Configure(
	computeClient *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSServerGroupClient {
	r.computeV2 = computeClient
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// Invokes servergroups.List() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) List(ctx context.Context, opts servergroups.ListOptsBuilder) ([]servergroups.ServerGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServerGroup, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	page, err := servergroups.List(r.computeV2, opts).AllPages(tctx)
	if err != nil {
		return nil, err
	}
	return servergroups.ExtractServerGroups(page)
}

// Invokes servergroups.Create() in gophercloud's servergroups package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) Create(ctx context.Context, opts servergroups.CreateOptsBuilder) (*servergroups.ServerGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServerGroup, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	srv, err := servergroups.Create(tctx, r.computeV2, opts).Extract()
	return srv, err
}

// Invokes servergroups.Get() in gophercloud's servergroups package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) Get(ctx context.Context, id string) (*servergroups.ServerGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServerGroup, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	srv, err := servergroups.Get(tctx, r.computeV2, id).Extract()
	return srv, err
}

// Invokes servergroups.Delete() in gophercloud's servergroups package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServerGroup, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	return servergroups.Delete(tctx, r.computeV2, id).ExtractErr()
}
