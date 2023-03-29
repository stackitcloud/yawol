package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PatchLBStatus patch loadbalancer status
func PatchServiceStatus(
	ctx context.Context,
	sw client.StatusWriter,
	svc *coreV1.Service,
	svcStatus *coreV1.ServiceStatus,
) error {
	svcStatusJSON, err := json.Marshal(svcStatus)
	if err != nil {
		return err
	}
	patch := []byte(`{"status":` + string(svcStatusJSON) + `}`)
	return sw.Patch(ctx, svc, client.RawPatch(types.MergePatchType, patch))
}

// GetDebugSettings return loadbalancer debug settings for a service
func GetDebugSettings(svc *coreV1.Service) yawolv1beta1.LoadBalancerDebugSettings {
	debugSettings := yawolv1beta1.LoadBalancerDebugSettings{}
	if val, _ := strconv.ParseBool(svc.Annotations[yawolv1beta1.ServiceDebug]); val {
		debugSettings.Enabled = true
		if svc.Annotations[yawolv1beta1.ServiceDebugSSHKey] != "" {
			debugSettings.SshkeyName = svc.Annotations[yawolv1beta1.ServiceDebugSSHKey]
		}
	}
	return debugSettings
}

// GetOptions return loadbalancer option settings for a service
func GetOptions(svc *coreV1.Service, recorder record.EventRecorder) yawolv1beta1.LoadBalancerOptions {
	options := yawolv1beta1.LoadBalancerOptions{}
	if svc.Annotations[yawolv1beta1.ServiceInternalLoadbalancer] != "" {
		options.InternalLB, _ = strconv.ParseBool(svc.Annotations[yawolv1beta1.ServiceInternalLoadbalancer])
	}

	if svc.Annotations[yawolv1beta1.ServiceLoadBalancerSourceRanges] != "" {
		options.LoadBalancerSourceRanges = strings.Split(svc.Annotations[yawolv1beta1.ServiceLoadBalancerSourceRanges], ",")
	}
	if svc.Spec.LoadBalancerSourceRanges != nil {
		options.LoadBalancerSourceRanges = svc.Spec.LoadBalancerSourceRanges
	}
	if svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocol] != "" {
		options.TCPProxyProtocol, _ = strconv.ParseBool(svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocol])
	}
	if svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocolPortsFilter] != "" {
		options.TCPProxyProtocolPortsFilter = getTCPProxyProtocolPortsFilter(
			svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocolPortsFilter],
		)
	}
	if val, _ := strconv.ParseBool(svc.Annotations[yawolv1beta1.ServiceLogForward]); val {
		options.LogForward.Enabled = true
		if svc.Annotations[yawolv1beta1.ServiceLogForwardLokiURL] != "" {
			options.LogForward.LokiURL = svc.Annotations[yawolv1beta1.ServiceLogForwardLokiURL]
		}
	}

	if svc.Annotations[yawolv1beta1.ServiceTCPIdleTimeout] != "" {
		tcpIdleTimeout, err := time.ParseDuration(svc.Annotations[yawolv1beta1.ServiceTCPIdleTimeout])
		if err != nil {
			recorder.Event(svc, coreV1.EventTypeWarning, "update",
				"Could not parse "+yawolv1beta1.ServiceTCPIdleTimeout+" to time duration. Ignoring option.")
		} else {
			options.TCPIdleTimeout = &metav1.Duration{Duration: tcpIdleTimeout}
		}
	}

	if svc.Annotations[yawolv1beta1.ServiceUDPIdleTimeout] != "" {
		udpIdleTimeout, err := time.ParseDuration(svc.Annotations[yawolv1beta1.ServiceUDPIdleTimeout])
		if err != nil {
			recorder.Event(svc, coreV1.EventTypeWarning, "update",
				"Could not parse "+yawolv1beta1.ServiceUDPIdleTimeout+" to time duration. Ignoring option.")
		} else {
			options.UDPIdleTimeout = &metav1.Duration{Duration: udpIdleTimeout}
		}
	}

	if svc.Annotations[yawolv1beta1.ServiceServerGroupPolicy] != "" {
		options.ServerGroupPolicy = svc.Annotations[yawolv1beta1.ServiceServerGroupPolicy]
	}

	return options
}

// GetIPFromStatus return ip from service (Status.LoadBalancer.Ingress[0].IP)
func GetIPFromStatus(svc *coreV1.Service) *string {
	if len(svc.Status.LoadBalancer.Ingress) > 0 &&
		svc.Status.LoadBalancer.Ingress[0].IP != "" {
		return &svc.Status.LoadBalancer.Ingress[0].IP
	}
	return nil
}

// GetExistingFloatingIPFromAnnotation return ip from the ServiceExistingFloatingIP annotation
func GetExistingFloatingIPFromAnnotation(svc *coreV1.Service) *string {
	if svc.Annotations[yawolv1beta1.ServiceExistingFloatingIP] != "" {
		existingIP := svc.Annotations[yawolv1beta1.ServiceExistingFloatingIP]
		return &existingIP
	}
	return nil
}

// GetReplicasFromService retruns replicas from Annotation. Default is 1
func GetReplicasFromService(service *coreV1.Service) int {
	replicaString, found := service.Annotations[yawolv1beta1.ServiceReplicas]
	if !found {
		return 1
	}

	replicas, err := strconv.Atoi(replicaString)
	if err != nil {
		return 1
	}
	return replicas
}

func GetLoadBalancerNameFromService(service *coreV1.Service) string {
	return service.Namespace + "--" + service.Name
}

// ValidateService checks if the service is valid
func ValidateService(svc *coreV1.Service) error {
	for _, port := range svc.Spec.Ports {
		switch port.Protocol {
		case coreV1.ProtocolTCP:
		case coreV1.ProtocolUDP:
		default:
			return fmt.Errorf("%w: %v)", ErrUnsupportedProtocol, port.Protocol)
		}
	}
	return nil
}

// getTCPProxyProtocolPortsFilter return port list from annotation
func getTCPProxyProtocolPortsFilter(tcpProxyProtocolPortsFilter string) []int32 {
	if tcpProxyProtocolPortsFilter == "" {
		return nil
	}
	var portFilter []int32
	for _, port := range strings.Split(tcpProxyProtocolPortsFilter, ",") {
		intPort, err := strconv.Atoi(port)
		if err != nil {
			return nil
		}
		portFilter = append(portFilter, int32(intPort)) //nolint:gosec // ints are always under int16
	}
	return portFilter
}
