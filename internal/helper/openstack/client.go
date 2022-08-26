package openstack

import (
	"context"
	"fmt"

	"github.com/stackitcloud/yawol/internal/openstack"

	coreV1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetOpenStackClientForAuthRef(
	ctx context.Context,
	c client.Client,
	authRef coreV1.SecretReference,
	getOsClientForIni func(iniData []byte) (openstack.Client, error),
) (openstack.Client, error) {
	// get openstack infrastructure secret
	var infraSecret coreV1.Secret
	err := c.Get(ctx, client.ObjectKey{
		Name:      authRef.Name,
		Namespace: authRef.Namespace,
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
	if osClient, err = getOsClientForIni(infraSecret.Data["cloudprovider.conf"]); err != nil {
		return nil, err
	}
	return osClient, nil
}
