// nolint: dupl // we can't extract this code because of generics
package openstack

import (
	"context"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/prometheus/client_golang/prometheus"
)

// The OSFloatingIPClient is a implementation for FipClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSFloatingIPClient struct {
	networkV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSFloatingIPClient) Configure(
	networkV2 *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSFloatingIPClient {
	r.networkV2 = networkV2
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// Invokes floatingips.List() in gophercloud's floating ip package and extracts all floating ips.
// Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) List(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectFloatingIP, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	pages, err := floatingips.List(r.networkV2, opts).AllPages(tctx)
	if err != nil {
		return nil, err
	}
	return floatingips.ExtractFloatingIPs(pages)
}

// Invokes floatingips.Create() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Create(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectFloatingIP, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	fip, err := floatingips.Create(tctx, r.networkV2, opts).Extract()
	return fip, err
}

// Invokes floatingips.Update() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Update(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectFloatingIP, MetricOperationUpdate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	fip, err := floatingips.Update(tctx, r.networkV2, id, opts).Extract()
	return fip, err
}

// Invokes floatingips.Get() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Get(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectFloatingIP, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	fip, err := floatingips.Get(tctx, r.networkV2, id).Extract()
	return fip, err
}

// Invokes floatingips.Delete() in gophercloud's floatingip package. Uses the networkV2 client provided in Configure().
func (r *OSFloatingIPClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectFloatingIP, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	err := floatingips.Delete(tctx, r.networkV2, id).ExtractErr()
	return err
}
