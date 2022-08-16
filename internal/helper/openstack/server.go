package openstack

import (
	"context"

	"dev.azure.com/schwarzit/schwarzit.ske/yawol.git/internal/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
)

const ServerStatusError = "ERROR"
const ServerStatusStopped = "STOPPED"
const ServerStatusActive = "ACTIVE"
const ServerStatusDeleted = "DELETED"

// GetServerByName returns a Server filtered By Name.
// Returns an error on connection issues.
// Returns nil if not found.
func GetServerByName(
	ctx context.Context,
	serverClient openstack.ServerClient,
	serverName string,
) (*servers.Server, error) {
	serverList, err := serverClient.List(ctx, servers.ListOpts{
		Name: serverName,
	})
	if err != nil {
		return nil, err
	}

	for i := range serverList {
		if serverList[i].Name == serverName {
			return &serverList[i], nil
		}
	}
	return nil, nil
}

func DeleteServer(
	ctx context.Context,
	serverClient openstack.ServerClient,
	serverID string,
) error {
	return serverClient.Delete(ctx, serverID)
}

func GetServerByID(
	ctx context.Context,
	serverClient openstack.ServerClient,
	serverID string,
) (*servers.Server, error) {
	return serverClient.Get(ctx, serverID)
}

func CreateServer(
	ctx context.Context,
	serverClient openstack.ServerClient,
	opts servers.CreateOptsBuilder,
) (*servers.Server, error) {
	return serverClient.Create(ctx, opts)
}
