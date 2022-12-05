package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/servergroups"
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

// Invokes servers.List() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) List(ctx context.Context, opts servergroups.ListOptsBuilder) ([]servergroups.ServerGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()
	page, err := servergroups.List(r.computeV2, opts).AllPages()
	if err != nil {
		return nil, err
	}
	return servergroups.ExtractServerGroups(page)
}

// Invokes servers.Create() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) Create(ctx context.Context, opts servergroups.CreateOptsBuilder) (*servergroups.ServerGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	srv, err := servergroups.Create(r.computeV2, opts).Extract()
	r.computeV2.Context = nil
	return srv, err
}

// Invokes servergroups.Get() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) Get(ctx context.Context, id string) (*servergroups.ServerGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	srv, err := servergroups.Get(r.computeV2, id).Extract()
	return srv, err
}

// Invokes servers.Delete() in gophercloud's servers package. Uses the computeV2 client provided in Configure().
func (r *OSServerGroupClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINova, MetricObjectServer, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	r.computeV2.Context = tctx
	defer func() {
		r.computeV2.Context = nil
	}()

	err := servergroups.Delete(r.computeV2, id).ExtractErr()
	return err
}
