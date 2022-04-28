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

	"dev.azure.com/schwarzit/schwarzit.ske/yawol.git/internal/openstack"
)

type MockClient struct {
	StoredValues             map[string]interface{}
	FipClientObj             openstack.FipClient
	PortClientObj            openstack.PortClient
	GroupClientObj           openstack.GroupClient
	RuleClientObj            openstack.RuleClient
	ServerClientObj          openstack.ServerClient
	KeyPairClientObj         openstack.KeyPairClient
	AttachInterfaceClientObj openstack.AttachInterfaceClient
	LoadBalancerClientObj    openstack.LoadBalancerClient
}

func (r *MockClient) Configure(ini []byte, timeout time.Duration) error {
	r.StoredValues = make(map[string]interface{})
	return nil
}
func (r *MockClient) FipClient(ctx context.Context) (openstack.FipClient, error) {
	return r.FipClientObj, nil
}
func (r *MockClient) PortClient(ctx context.Context) (openstack.PortClient, error) {
	return r.PortClientObj, nil
}
func (r *MockClient) GroupClient(ctx context.Context) (openstack.GroupClient, error) {
	return r.GroupClientObj, nil
}
func (r *MockClient) RuleClient(ctx context.Context) (openstack.RuleClient, error) {
	return r.RuleClientObj, nil
}
func (r *MockClient) ServerClient(ctx context.Context) (openstack.ServerClient, error) {
	return r.ServerClientObj, nil
}
func (r *MockClient) KeyPairClient(ctx context.Context) (openstack.KeyPairClient, error) {
	return r.KeyPairClientObj, nil
}
func (r *MockClient) AttachInterfaceClient(ctx context.Context) (openstack.AttachInterfaceClient, error) {
	return r.AttachInterfaceClientObj, nil
}
func (r *MockClient) LoadBalancerClient(ctx context.Context) (openstack.LoadBalancerClient, error) {
	return r.LoadBalancerClientObj, nil
}
