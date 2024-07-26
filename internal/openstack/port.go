package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
)

// The OSPortClient is a implementation for PortClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSPortClient struct {
	networkV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSPortClient) Configure(
	networkClient *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSPortClient {
	r.networkV2 = networkClient
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// Invokes ports.List() in gophercloud's ports package and extracts all ports.
// Uses the networkV2 client provided in Configure().
func (r *OSPortClient) List(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectPort, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	page, err := ports.List(r.networkV2, opts).AllPages(tctx)
	if err != nil {
		return nil, err
	}
	return ports.ExtractPorts(page)
}

// Invokes groups.Get() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Get(ctx context.Context, id string) (*ports.Port, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectPort, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	port, err := ports.Get(tctx, r.networkV2, id).Extract()
	return port, err
}

// Invokes groups.Create() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Create(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectPort, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	port, err := ports.Create(tctx, r.networkV2, opts).Extract()
	return port, err
}

// Invokes groups.Update() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Update(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectPort, MetricOperationUpdate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	port, err := ports.Update(tctx, r.networkV2, id, opts).Extract()
	return port, err
}

// Invokes groups.Delete() in gophercloud's ports package. Uses the networkV2 client provided in Configure().
func (r *OSPortClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectPort, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	err := ports.Delete(tctx, r.networkV2, id).ExtractErr()
	return err
}
