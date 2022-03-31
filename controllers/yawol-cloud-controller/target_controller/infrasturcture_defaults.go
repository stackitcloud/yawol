package target_controller

import (
	"strconv"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	v1 "k8s.io/api/core/v1"
)

type InfrastructureDefaults struct {
	AuthSecretName    *string
	FloatingNetworkId *string
	NetworkId         *string
	Namespace         *string
	FlavorRef         *yawolv1beta1.OpenstackFlavorRef
	ImageRef          *yawolv1beta1.OpenstackImageRef
	InternalLB        *bool
}

func GetMergedInfrastructureDetails(defaults InfrastructureDefaults, svc v1.Service) InfrastructureDefaults {
	svcConfig := GetInfrastructureDetailsFromService(svc)

	if svcConfig.Namespace != nil {
		defaults.Namespace = svcConfig.Namespace
	}

	if svcConfig.InternalLB != nil {
		defaults.InternalLB = svcConfig.InternalLB
	}

	if svcConfig.ImageRef != nil {
		defaults.ImageRef = svcConfig.ImageRef
	}

	if svcConfig.FlavorRef != nil {
		defaults.FlavorRef = svcConfig.FlavorRef
	}

	if svcConfig.NetworkId != nil {
		defaults.NetworkId = svcConfig.NetworkId
	}

	if svcConfig.FloatingNetworkId != nil {
		defaults.FloatingNetworkId = svcConfig.FloatingNetworkId
	}

	if svcConfig.AuthSecretName != nil {
		defaults.AuthSecretName = svcConfig.AuthSecretName
	}

	return defaults
}

func GetInfrastructureDetailsFromService(svc v1.Service) InfrastructureDefaults {
	serviceInfraDefault := InfrastructureDefaults{
		AuthSecretName:    nil,
		FloatingNetworkId: nil,
		NetworkId:         nil,
		FlavorRef:         nil,
		ImageRef:          nil,
		InternalLB:        nil,
	}
	if svc.Annotations[yawolv1beta1.ServiceImageID] != "" {
		imageID := svc.Annotations[yawolv1beta1.ServiceImageID]
		serviceInfraDefault.ImageRef = &yawolv1beta1.OpenstackImageRef{
			ImageID: &imageID,
		}
	}
	if svc.Annotations[yawolv1beta1.ServiceFlavorID] != "" {
		flavorID := svc.Annotations[yawolv1beta1.ServiceFlavorID]
		serviceInfraDefault.FlavorRef = &yawolv1beta1.OpenstackFlavorRef{
			FlavorID: &flavorID,
		}
	}
	if svc.Annotations[yawolv1beta1.ServiceInternalLoadbalancer] != "" {
		internalLB, err := strconv.ParseBool(svc.Annotations[yawolv1beta1.ServiceInternalLoadbalancer])
		if err == nil {
			serviceInfraDefault.InternalLB = &internalLB
		}
	}
	// todo add more settings when required
	return serviceInfraDefault
}
