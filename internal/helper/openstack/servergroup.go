package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servergroups"
	"github.com/stackitcloud/yawol/internal/openstack"
)

// GetServerGroupByName returns a server group filtered By Name.
// Returns an error on connection issues.
// Returns nil if not found.
func GetServerGroupByName(
	ctx context.Context,
	serverGroupClient openstack.ServerGroupClient,
	serverGroupName string,
) (*servergroups.ServerGroup, error) {
	serverGroupList, err := serverGroupClient.List(ctx, servergroups.ListOpts{})
	if err != nil {
		return nil, err
	}

	for i := range serverGroupList {
		if serverGroupList[i].Name == serverGroupName {
			return &serverGroupList[i], nil
		}
	}

	return nil, nil
}

// CreateServerGroup creates a server group in openstack
func CreateServerGroup(
	ctx context.Context,
	serverGroupClient openstack.ServerGroupClient,
	serverGroupName string,
	affinity string,
) (*servergroups.ServerGroup, error) {
	serverGroup, err := serverGroupClient.Create(ctx, servergroups.CreateOpts{
		Name:     serverGroupName,
		Policies: []string{affinity},
	})
	if err != nil {
		return nil, err
	}
	return serverGroup, nil
}

// GetServerGroupByID returns a server group by an openstack ID.
// Returns an error on connection issues.
// Returns err if not found.
func GetServerGroupByID(
	ctx context.Context,
	serverGroupClient openstack.ServerGroupClient,
	serverGroupID string,
) (*servergroups.ServerGroup, error) {
	serverGroup, err := serverGroupClient.Get(ctx, serverGroupID)
	if err != nil {
		return nil, err
	}
	return serverGroup, err
}

// DeleteServerGroup deletes a server group by ID
func DeleteServerGroup(
	ctx context.Context,
	serverGroupClient openstack.ServerGroupClient,
	serverGroupID string,
) error {
	return serverGroupClient.Delete(ctx, serverGroupID)
}
