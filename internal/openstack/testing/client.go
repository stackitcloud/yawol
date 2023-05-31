/*
This package is used for testing. It contains MockClients to intercept calls, intended to go to openstack.

Example usage

	mockClient := testing.MockClient{}
	mockClient.StoredValues = map[string]interface{}{}
	mockClient.GroupClientObj = &testing.CallbackGroupClient{
		ListFunc: func(opts groups.ListOpts) ([]groups.SecGroup, error) {
			return []groups.SecGroup{
				{
					Name:  "sec-group-name",
					ID:    "sec-group-id",
					Rules: desiredRules,
				},
			}, nil
		},
	}
*/
package testing

import (
	"context"
	"time"

	"github.com/stackitcloud/yawol/internal/openstack"

	"github.com/prometheus/client_golang/prometheus"
)

type MockClient struct {
	StoredValues         map[string]interface{}
	FipClientObj         openstack.FipClient
	PortClientObj        openstack.PortClient
	GroupClientObj       openstack.GroupClient
	RuleClientObj        openstack.RuleClient
	ServerClientObj      openstack.ServerClient
	KeyPairClientObj     openstack.KeyPairClient
	ServerGroupClientObj openstack.ServerGroupClient
}

func (r *MockClient) Configure(_ []byte, _ openstack.OSClientOverwrite, _ time.Duration, _ *prometheus.CounterVec) error {
	r.StoredValues = make(map[string]interface{})
	return nil
}
func (r *MockClient) FipClient(_ context.Context) (openstack.FipClient, error) {
	return r.FipClientObj, nil
}
func (r *MockClient) PortClient(_ context.Context) (openstack.PortClient, error) {
	return r.PortClientObj, nil
}
func (r *MockClient) GroupClient(_ context.Context) (openstack.GroupClient, error) {
	return r.GroupClientObj, nil
}
func (r *MockClient) RuleClient(_ context.Context) (openstack.RuleClient, error) {
	return r.RuleClientObj, nil
}
func (r *MockClient) ServerClient(_ context.Context) (openstack.ServerClient, error) {
	return r.ServerClientObj, nil
}
func (r *MockClient) KeyPairClient(_ context.Context) (openstack.KeyPairClient, error) {
	return r.KeyPairClientObj, nil
}
func (r *MockClient) ServerGroupClient(_ context.Context) (openstack.ServerGroupClient, error) {
	return r.ServerGroupClientObj, nil
}
