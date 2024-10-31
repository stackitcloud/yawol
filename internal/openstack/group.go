// nolint: dupl // we can't extract this code because of generics
package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
)

// The OSGroupClient is a implementation for GroupClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSGroupClient struct {
	networkV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSGroupClient) Configure(
	networkClient *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSGroupClient {
	r.networkV2 = networkClient
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// List Invokes groups.List() in gophercloud's groups package and extracts all security groups.
// Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) List(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectGroup, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	pages, err := groups.List(r.networkV2, opts).AllPages(tctx)
	if err != nil {
		return nil, err
	}
	return groups.ExtractGroups(pages)
}

// Create Invokes groups.Create() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Create(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectGroup, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	group, err := groups.Create(tctx, r.networkV2, opts).Extract()
	return group, err
}

// Update Invokes groups.Update() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Update(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectGroup, MetricOperationUpdate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	group, err := groups.Update(tctx, r.networkV2, id, opts).Extract()
	return group, err
}

// Get Invokes groups.Get() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Get(ctx context.Context, id string) (*groups.SecGroup, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectGroup, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	group, err := groups.Get(tctx, r.networkV2, id).Extract()
	return group, err
}

// Delete Invokes groups.Delete() in gophercloud's groups package. Uses the networkV2 client provided in Configure().
func (r *OSGroupClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectGroup, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	err := groups.Delete(tctx, r.networkV2, id).ExtractErr()
	return err
}
