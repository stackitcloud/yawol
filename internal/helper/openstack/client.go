package openstack

import (
	"context"
	"fmt"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"

	"github.com/stackitcloud/yawol/internal/openstack"

	coreV1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetOpenStackClientForInfrastructure(
	ctx context.Context,
	c client.Client,
	infra yawolv1beta1.LoadBalancerInfrastructure,
	getOsClientForIni openstack.GetOSClientFunc,
) (openstack.Client, error) {
	// get openstack infrastructure secret
	var infraSecret coreV1.Secret
	err := c.Get(ctx, client.ObjectKey{
		Name:      infra.AuthSecretRef.Name,
		Namespace: infra.AuthSecretRef.Namespace,
	}, &infraSecret)
	if err != nil {
		return nil, err
	}

	// check if secret contains needed data
	if _, ok := infraSecret.Data["cloudprovider.conf"]; !ok {
		return nil, fmt.Errorf("cloudprovider.conf not found in secret")
	}

	// create openstack client from secret
	var osClient openstack.Client
	if osClient, err = getOsClientForIni(
		infraSecret.Data["cloudprovider.conf"],
		openstack.OSClientOverwrite{ProjectID: infra.ProjectID},
	); err != nil {
		return nil, err
	}
	return osClient, nil
}
