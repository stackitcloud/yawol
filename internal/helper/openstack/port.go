package openstack

import (
	"context"

	"github.com/stackitcloud/yawol/internal/openstack"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
)

// GetPortByName returns a Port filtered By Name.
// Returns an error on connection issues.
// Returns nil if not found.
func GetPortByName(
	ctx context.Context,
	portClient openstack.PortClient,
	portName string,
) (*ports.Port, error) {
	portList, err := portClient.List(ctx, ports.ListOpts{
		Name: portName,
	})
	if err != nil {
		return nil, err
	}

	for i := range portList {
		if portList[i].Name == portName {
			return &portList[i], nil
		}
	}

	return nil, nil
}

// CreatePort creates a port in openstack
func CreatePort(
	ctx context.Context,
	portClient openstack.PortClient,
	portName string,
	networkID string,
	subnetID string,
) (*ports.Port, error) {
	opts := ports.CreateOpts{
		Name:      portName,
		NetworkID: networkID,
	}
	if subnetID != "" {
		opts.FixedIPs = []ports.IP{
			{SubnetID: subnetID},
		}
	}
	port, err := portClient.Create(ctx, opts)
	if err != nil {
		return nil, err
	}
	return port, nil
}

// GetPortByID returns a port by an openstack ID.
// Returns an error on connection issues.
// Returns err if not found.
func GetPortByID(
	ctx context.Context,
	portClient openstack.PortClient,
	portID string,
) (*ports.Port, error) {
	port, err := portClient.Get(ctx, portID)
	if err != nil {
		return nil, err
	}
	return port, err
}

// GetAllPorts returns all ports that can be shown.
func GetAllPorts(
	ctx context.Context,
	portClient openstack.PortClient,
) ([]ports.Port, error) {
	return portClient.List(ctx, ports.ListOpts{})
}

// DeletePort deletes a port by ID
func DeletePort(
	ctx context.Context,
	portClient openstack.PortClient,
	portID string,
) error {
	return portClient.Delete(ctx, portID)
}

// BindSecGroupToPortIfNeeded binds a SecGroup to a port if needed.
func SetAllowedAddressPairsInPort(
	ctx context.Context,
	portClient openstack.PortClient,
	port *ports.Port,
	addressPair *[]ports.AddressPair,
) error {
	if addressPair == nil {
		return nil
	}
	// check if security groups are attached to port
	_, err := portClient.Update(ctx, port.ID, ports.UpdateOpts{
		AllowedAddressPairs: addressPair,
	})
	return err
}

// BindSecGroupToPortIfNeeded binds a SecGroup to a port if needed.
func BindSecGroupToPortIfNeeded(
	ctx context.Context,
	portClient openstack.PortClient,
	secGroup *string,
	port *ports.Port,
) (bool, error) {
	// check if security groups are attached to port
	if secGroup == nil {
		return false, nil
	}
	if len(port.SecurityGroups) == 1 && port.SecurityGroups[0] == *secGroup {
		return false, nil
	}

	if _, err := portClient.Update(ctx, port.ID, ports.UpdateOpts{
		SecurityGroups: &[]string{*secGroup},
	}); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveSecGroupFromPortIfNeeded removes a SecGroup from a port if needed.
func RemoveSecGroupFromPortIfNeeded(
	ctx context.Context,
	portClient openstack.PortClient,
	port *ports.Port,
	secGroupID string,
) error {
	changed := false
	var purgedList []string
	for _, secGroup := range port.SecurityGroups {
		if secGroup == secGroupID {
			changed = true
			continue
		}
		purgedList = append(purgedList, secGroup)
	}

	if !changed {
		return nil
	}

	_, err := portClient.Update(ctx, port.ID, ports.UpdateOpts{
		SecurityGroups: &purgedList,
	})

	return err
}
