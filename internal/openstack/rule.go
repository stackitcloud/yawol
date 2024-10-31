package openstack

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
)

// The OSRuleClient is a implementation for RuleClient. When you want to use this struct be sure to call
// Configure() before calling any other method. Otherwise it will result in errors.
//
// As an easier abstraction you can use OSClient in this package, where you can insert data from an ini
// file to automatically initialize all modules you want to use.
type OSRuleClient struct {
	networkV2   *gophercloud.ServiceClient
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configure takes NetworkV2 ServiceClient to receive endpoints and auth info for further calls against openstack.
func (r *OSRuleClient) Configure(
	networkClient *gophercloud.ServiceClient,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) *OSRuleClient {
	r.networkV2 = networkClient
	r.timeout = timeout
	r.promCounter = promCounter
	return r
}

// Invokes rules.List() in gophercloud's rules package and extracts all security groups.
// Uses the networkV2 client provided in Configure().
func (r *OSRuleClient) List(ctx context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectRule, MetricOperationList)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	page, err := rules.List(r.networkV2, opts).AllPages(tctx)
	if err != nil {
		return nil, err
	}
	return rules.ExtractRules(page)
}

// Invokes rules.Create() in gophercloud's rules package and extracts all security groups.
// Uses the networkV2 client provided in Configure().
func (r *OSRuleClient) Create(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectRule, MetricOperationCreate)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rule, err := rules.Create(tctx, r.networkV2, opts).Extract()
	return rule, err
}

// Invokes rules.Get() in gophercloud's rules package and extracts all security groups.
// Uses the networkV2 client provided in Configure().
func (r *OSRuleClient) Get(ctx context.Context, id string) (*rules.SecGroupRule, error) {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectRule, MetricOperationGet)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rule, err := rules.Get(tctx, r.networkV2, id).Extract()
	return rule, err
}

// Invokes rules.Delete() in gophercloud's rules package
// Uses the networkV2 client provided in Configure().
func (r *OSRuleClient) Delete(ctx context.Context, id string) error {
	increasePromCounter(r.promCounter, MetricAPINeutron, MetricObjectRule, MetricOperationDelete)
	tctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	err := rules.Delete(tctx, r.networkV2, id).ExtractErr()
	return err
}
