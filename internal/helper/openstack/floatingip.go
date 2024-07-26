package openstack

import (
	"context"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/openstack"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
)

// CreateFIP creates a FIP and returns it.
func CreateFIP(
	ctx context.Context,
	fipClient openstack.FipClient,
	lb *yawolv1beta1.LoadBalancer,
) (*floatingips.FloatingIP, error) {
	// TODO cleanup after removing deprecated fields
	var floatingNetID string
	if lb.Spec.Infrastructure.FloatingNetID != nil { //nolint: staticcheck // needed to be backwards compatible
		floatingNetID = *lb.Spec.Infrastructure.FloatingNetID //nolint: staticcheck // needed to be backwards compatible
	}
	if lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID != nil {
		floatingNetID = *lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID
	}

	fip, err := fipClient.Create(ctx, floatingips.CreateOpts{
		Description:       *lb.Status.FloatingName,
		FloatingNetworkID: floatingNetID,
	})
	if err != nil {
		return nil, err
	}
	return fip, nil
}

// DeleteFIP deletes a FIP and returns the error.
func DeleteFIP(
	ctx context.Context,
	fipClient openstack.FipClient,
	fipID string,
) error {
	return fipClient.Delete(ctx, fipID)
}

// GetFIPByName returns a FIP filtered By Name.
// Returns an error on connection issues.
// Returns nil if not found.
func GetFIPByName(
	ctx context.Context,
	fipClient openstack.FipClient,
	fipName string,
) (*floatingips.FloatingIP, error) {
	fipList, err := fipClient.List(ctx, floatingips.ListOpts{
		Description: fipName,
	})
	if err != nil {
		return nil, err
	}

	for i := range fipList {
		if fipList[i].Description == fipName {
			return &fipList[i], nil
		}
	}

	return nil, helper.ErrFIPNotFound
}

// GetFIPByIP returns a FIP filtered By IP.
// Returns an error on connection issues.
// Returns nil if not found.
func GetFIPByIP(
	ctx context.Context,
	fipClient openstack.FipClient,
	fipIP string,
) (*floatingips.FloatingIP, error) {
	fipList, err := fipClient.List(ctx, floatingips.ListOpts{
		FloatingIP: fipIP,
	})
	if err != nil {
		return nil, err
	}

	for i := range fipList {
		if fipList[i].FloatingIP == fipIP {
			return &fipList[i], nil
		}
	}

	return nil, helper.ErrFIPNotFound
}

// GetFIPByID returns a FIP filtered by an openstack ID.
// Returns an error on connection issues.
// Returns err if not found.
func GetFIPByID(
	ctx context.Context,
	fipClient openstack.FipClient,
	fipID string,
) (*floatingips.FloatingIP, error) {
	fip, err := fipClient.Get(ctx, fipID)
	if err != nil {
		return nil, err
	}
	return fip, nil
}

// BindFIPToPort binds a fip to a port
func BindFIPToPort(
	ctx context.Context,
	fipClient openstack.FipClient,
	fipID string,
	portID *string,
) error {
	_, err := fipClient.Update(ctx, fipID, floatingips.UpdateOpts{
		PortID: portID,
	})
	return err
}
