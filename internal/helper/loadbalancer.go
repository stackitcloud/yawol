package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/prometheus/client_golang/prometheus"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerOpenstackReconcileIsNeeded returns true if an openstack reconcile is needed.
func LoadBalancerOpenstackReconcileIsNeeded(lb *yawolv1beta1.LoadBalancer) bool {
	// LastOpenstackReconcile is nil, first run
	if lb.Status.LastOpenstackReconcile == nil {
		return true
	}

	// lastOpenstackReconcile is older than 5 min
	// add some seconds in order to be sure it reconciles
	if lb.Status.LastOpenstackReconcile.Before(&metaV1.Time{Time: time.Now().Add(-OpenstackReconcileTime).Add(2 * time.Second)}) {
		return true
	}

	// loadbalancer is in initial creation and not ready
	if lb.Status.ExternalIP == nil && lb.Status.ReadyReplicas != nil && *lb.Status.ReadyReplicas > 0 {
		return true
	}

	// internalLB but has FloatingIP
	if lb.Spec.Options.InternalLB && (lb.Status.FloatingID != nil || lb.Status.FloatingName != nil) {
		return true
	}

	// no internalLB but has no FloatingIP
	if !lb.Spec.Options.InternalLB && (lb.Status.FloatingID == nil || lb.Status.FloatingName == nil) {
		return true
	}

	openstackReconcileHash, err := GetOpenStackReconcileHash(lb)
	// OpenstackReconcileHash could not be created
	if err != nil {
		return true
	}

	// OpenstackReconcileHash is nil or has changed
	if lb.Status.OpenstackReconcileHash == nil || *lb.Status.OpenstackReconcileHash != openstackReconcileHash {
		return true
	}

	return false
}

func GetLoadBalancerForLoadBalancerSet(
	ctx context.Context,
	c client.Client,
	loadBalancerSet *yawolv1beta1.LoadBalancerSet,
) (*yawolv1beta1.LoadBalancer, error) {
	if loadBalancerSet.OwnerReferences == nil {
		return nil, nil
	}
	for _, ref := range loadBalancerSet.OwnerReferences {
		if ref.Kind != LoadBalancerKind {
			continue
		}
		var lb yawolv1beta1.LoadBalancer
		err := c.Get(ctx, types.NamespacedName{
			Namespace: loadBalancerSet.Namespace,
			Name:      ref.Name,
		}, &lb)
		return &lb, err
	}
	return nil, nil
}

// GetOpenStackReconcileHash returns a 16 char hash for all openstack relevant data to check if an openstack reconcile is needed.
func GetOpenStackReconcileHash(lb *yawolv1beta1.LoadBalancer) (string, error) {
	adHocDebug, _ := strconv.ParseBool(lb.Annotations[yawolv1beta1.LoadBalancerAdHocDebug])
	return HashData(map[string]interface{}{
		"ports":             lb.Spec.Ports,
		"sourceRanges":      lb.Spec.Options.LoadBalancerSourceRanges,
		"debugSettings":     lb.Spec.DebugSettings,
		"serverGroupPolicy": lb.Spec.Options.ServerGroupPolicy,
		"adHocDebug":        adHocDebug,
	})
}

// RemoveFromLBStatus removes key from loadbalancer status.
func RemoveFromLBStatus(ctx context.Context, sw client.StatusWriter, lb *yawolv1beta1.LoadBalancer, key string) error {
	patch := []byte(`{"status":{"` + key + `": null}}`)
	return sw.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

// PatchLBStatus patch loadbalancer status
func PatchLBStatus(
	ctx context.Context,
	sw client.StatusWriter,
	lb *yawolv1beta1.LoadBalancer,
	lbStatus yawolv1beta1.LoadBalancerStatus,
) error {
	lbStatusJSON, err := json.Marshal(lbStatus)
	if err != nil {
		return err
	}
	patch := []byte(`{"status":` + string(lbStatusJSON) + `}`)
	return sw.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

// GetOwnersReferenceForLB returns OwnerReference for LoadBalancer
func GetOwnersReferenceForLB(lb *yawolv1beta1.LoadBalancer) metaV1.OwnerReference {
	return metaV1.OwnerReference{
		APIVersion: lb.APIVersion,
		Kind:       lb.Kind,
		Name:       lb.Name,
		UID:        lb.UID,
	}
}

func PatchLoadBalancerRevision(ctx context.Context, c client.Client, lb *yawolv1beta1.LoadBalancer, revision int) error {
	if revision < 1 {
		return ErrInvalidRevision
	}

	patch := []byte(`{"metadata": {"annotations": {"` + RevisionAnnotation + `": "` + strconv.Itoa(revision) + `"} }}`)
	return c.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func ReadRevisionFromLBS(lbs *yawolv1beta1.LoadBalancerSet) (int, error) {
	var currentRevision int
	if lbs.Annotations[RevisionAnnotation] == "" {
		return 0, nil
	}

	var err error
	if currentRevision, err = strconv.Atoi(lbs.Annotations[RevisionAnnotation]); err != nil {
		return 0, fmt.Errorf("%w: %s", ErrFailToReadRevisionFromAnnotation, err)
	}
	return currentRevision, nil
}

func ReadCurrentRevisionFromLB(lb *yawolv1beta1.LoadBalancer) (int, error) {
	var currentRevision int
	if lb.Annotations[RevisionAnnotation] == "" {
		return 0, nil
	}

	var err error
	if currentRevision, err = strconv.Atoi(lb.Annotations[RevisionAnnotation]); err != nil {
		return 0, ErrFailToReadRevisionFromAnnotation
	}
	return currentRevision, nil
}

func GetNextRevisionForLoadBalancer(setList *yawolv1beta1.LoadBalancerSetList) (int, error) {
	var highestRevision int
	for i := range setList.Items {
		rev, err := ReadRevisionFromLBS(&setList.Items[i])
		if err != nil {
			return 0, err
		}
		if rev > highestRevision {
			highestRevision = rev
		}
	}
	highestRevision++
	return highestRevision, nil
}

// GetLoadBalancerSourceRanges returns the LoadBalancerSourceRanges from the spec.
// If not set it uses "0.0.0.0/0", "::/0" as default to enable all sources.
func GetLoadBalancerSourceRanges(lb *yawolv1beta1.LoadBalancer) []string {
	if len(lb.Spec.Options.LoadBalancerSourceRanges) >= 1 {
		return lb.Spec.Options.LoadBalancerSourceRanges
	}
	return []string{"0.0.0.0/0", "::/0"}
}

// GetDesiredSecGroupRules returns all SecGroupRules that are needed.
// Based on default rules, ports, debug settings.
func GetDesiredSecGroupRulesForLoadBalancer(r record.EventRecorder, lb *yawolv1beta1.LoadBalancer, secGroupID string) []rules.SecGroupRule {
	desiredSecGroups := []rules.SecGroupRule{}

	for _, etherType := range []rules.RuleEtherType{rules.EtherType4, rules.EtherType6} {
		// icmpProtocol is different in ipv6, openstack accept it, but change it to ipv6-icmp.
		// It lets delete the rule each time, because it not pass the unused check.
		icmpProtocol := rules.ProtocolICMP
		if etherType == rules.EtherType6 {
			icmpProtocol = rules.ProtocolIPv6ICMP
		}

		// Egress all
		// VRRP for keepalived
		// Ping for easy debugging
		desiredSecGroups = append(
			desiredSecGroups,
			rules.SecGroupRule{
				EtherType: string(etherType),
				Direction: string(rules.DirEgress),
			},
			rules.SecGroupRule{
				EtherType:     string(etherType),
				Direction:     string(rules.DirIngress),
				Protocol:      string(rules.ProtocolVRRP),
				RemoteGroupID: secGroupID,
			},
			rules.SecGroupRule{
				EtherType:    string(etherType),
				Direction:    string(rules.DirIngress),
				Protocol:     string(icmpProtocol),
				PortRangeMin: 1,
				PortRangeMax: 8,
			})
	}

	desiredSecGroups = append(desiredSecGroups, getSecGroupRulesForPorts(r, lb)...)

	desiredSecGroups = append(desiredSecGroups, getSecGroupRulesForDebugSettings(r, lb)...)

	return desiredSecGroups
}

func getSecGroupRulesForPorts(r record.EventRecorder, lb *yawolv1beta1.LoadBalancer) []rules.SecGroupRule {
	portSecGroups := []rules.SecGroupRule{}

	sourceRanges := GetLoadBalancerSourceRanges(lb)

	for _, port := range lb.Spec.Ports {
		for _, cidr := range sourceRanges {
			// validate CIDR and ignore if invalid
			_, _, err := net.ParseCIDR(cidr)
			if err != nil {
				r.Event(
					lb, "Warning", "Error",
					"Could not parse LoadBalancerSourceRange: "+cidr,
				)
				continue
			}

			var etherType rules.RuleEtherType
			if strings.Contains(cidr, ".") {
				etherType = rules.EtherType4
			} else if strings.Contains(cidr, ":") {
				etherType = rules.EtherType6
			}

			portValue := int(port.Port)
			rule := rules.SecGroupRule{
				Direction:      string(rules.DirIngress),
				EtherType:      string(etherType),
				PortRangeMin:   portValue,
				PortRangeMax:   portValue,
				RemoteIPPrefix: cidr,
				Protocol:       strings.ToLower(string(port.Protocol)),
			}
			portSecGroups = append(portSecGroups, rule)
		}
	}
	return portSecGroups
}

func getSecGroupRulesForDebugSettings(r record.EventRecorder, lb *yawolv1beta1.LoadBalancer) []rules.SecGroupRule {
	adHocDebug, _ := strconv.ParseBool(lb.Annotations[yawolv1beta1.LoadBalancerAdHocDebug])

	if !lb.Spec.DebugSettings.Enabled &&
		!adHocDebug {
		return []rules.SecGroupRule{}
	}

	// check if port 22 is already in use
	for _, port := range lb.Spec.Ports {
		if port.Port == int32(22) {
			r.Event(
				lb, "Warning", "Warning",
				"DebugSettings are enabled but port 22 is already in use.",
			)
			return []rules.SecGroupRule{}
		}
	}

	r.Event(
		lb, "Warning", "Warning",
		"DebugSettings are enabled, Port 22 is open to all IP ranges.",
	)

	return []rules.SecGroupRule{{
		Direction:      string(rules.DirIngress),
		EtherType:      string(rules.EtherType4),
		RemoteIPPrefix: "0.0.0.0/0",
		PortRangeMin:   22,
		PortRangeMax:   22,
		Protocol:       string(rules.ProtocolTCP),
	}, {
		Direction:      string(rules.DirIngress),
		EtherType:      string(rules.EtherType6),
		RemoteIPPrefix: "::/0",
		PortRangeMin:   22,
		PortRangeMax:   22,
		Protocol:       string(rules.ProtocolTCP),
	}}
}

func ParseLoadBalancerMetrics(
	lb yawolv1beta1.LoadBalancer,
	metrics *helpermetrics.LoadBalancerMetricList,
) {
	if metrics == nil {
		return
	}
	parseLoadBalancerInfoMetric(lb, metrics.InfoMetrics)
	parseLoadBalancerOpenstackMetric(lb, metrics.OpenstackInfoMetrics)
	parseLoadBalancerReadyMetric(lb, metrics.ReplicasMetrics, metrics.ReplicasCurrentMetrics, metrics.ReplicasReadyMetrics)
	parseLoadBalancerDeletionTimestampMetric(lb, metrics.DeletionTimestampMetrics)
}

func parseLoadBalancerInfoMetric(
	lb yawolv1beta1.LoadBalancer,
	loadbalancerInfoMetric *prometheus.GaugeVec,
) {
	if loadbalancerInfoMetric == nil {
		return
	}

	labels := map[string]string{
		"lb":               lb.Name,
		"namespace":        lb.Namespace,
		"isInternal":       strconv.FormatBool(lb.Spec.Options.InternalLB),
		"tcpProxyProtocol": strconv.FormatBool(lb.Spec.Options.TCPProxyProtocol),
		"externalIP":       "nil",
		"tcpIdleTimeout":   "nil",
		"udpIdleTimeout":   "nil",
		"lokiEnabled":      strconv.FormatBool(lb.Spec.Options.LogForward.Enabled),
	}

	if lb.Status.ExternalIP != nil {
		labels["externalIP"] = *lb.Status.ExternalIP
	}

	if lb.Spec.Options.TCPIdleTimeout != nil {
		labels["tcpIdleTimeout"] = lb.Spec.Options.TCPIdleTimeout.String()
	}

	if lb.Spec.Options.UDPIdleTimeout != nil {
		labels["udpIdleTimeout"] = lb.Spec.Options.UDPIdleTimeout.String()
	}

	loadbalancerInfoMetric.DeletePartialMatch(map[string]string{"lb": lb.Name, "namespace": lb.Namespace})
	loadbalancerInfoMetric.With(labels).Set(1)
}

func parseLoadBalancerOpenstackMetric(
	lb yawolv1beta1.LoadBalancer,
	loadbalancerOpenstackMetric *prometheus.GaugeVec,
) {
	if loadbalancerOpenstackMetric == nil {
		return
	}
	labels := map[string]string{
		"lb":              lb.Name,
		"namespace":       lb.Namespace,
		"portID":          "nil",
		"floatingID":      "nil",
		"securityGroupID": "nil",
		"flavorID":        "nil",
	}

	if lb.Spec.Infrastructure.Flavor.FlavorID != nil {
		labels["flavorID"] = *lb.Spec.Infrastructure.Flavor.FlavorID
	}

	if lb.Status.PortID != nil {
		labels["portID"] = *lb.Status.PortID
	}

	if lb.Status.FloatingID != nil {
		labels["floatingID"] = *lb.Status.FloatingID
	}

	if lb.Status.SecurityGroupID != nil {
		labels["securityGroupID"] = *lb.Status.SecurityGroupID
	}

	loadbalancerOpenstackMetric.DeletePartialMatch(map[string]string{"lb": lb.Name, "namespace": lb.Namespace})
	loadbalancerOpenstackMetric.With(labels).Set(1)
}

func parseLoadBalancerReadyMetric(
	lb yawolv1beta1.LoadBalancer,
	loadBalancerReplicasMetrics *prometheus.GaugeVec,
	loadBalancerReplicasCurrentMetrics *prometheus.GaugeVec,
	loadBalancerReplicasReadyMetrics *prometheus.GaugeVec,
) {
	if loadBalancerReplicasMetrics == nil ||
		loadBalancerReplicasCurrentMetrics == nil ||
		loadBalancerReplicasReadyMetrics == nil {
		return
	}

	specReplicas := lb.Spec.Replicas
	if lb.DeletionTimestamp != nil {
		// If deletionTimestamp is set, the desired state is to have 0 replicas. Setting this metrics to 0 makes it easier
		// to define alerts that should not trigger false positives, if a LoadBalancer takes a long time to delete.
		specReplicas = 0
	}
	loadBalancerReplicasMetrics.WithLabelValues(lb.Name, lb.Namespace).Set(float64(specReplicas))

	if lb.Status.Replicas != nil {
		loadBalancerReplicasCurrentMetrics.WithLabelValues(lb.Name, lb.Namespace).Set(float64(*lb.Status.Replicas))
	}
	if lb.Status.ReadyReplicas != nil {
		loadBalancerReplicasReadyMetrics.WithLabelValues(lb.Name, lb.Namespace).Set(float64(*lb.Status.ReadyReplicas))
	}
}

func parseLoadBalancerDeletionTimestampMetric(
	lb yawolv1beta1.LoadBalancer,
	loadBalancerDeletionTimestampMetrics *prometheus.GaugeVec,
) {
	if loadBalancerDeletionTimestampMetrics == nil {
		return
	}

	if lb.DeletionTimestamp != nil {
		loadBalancerDeletionTimestampMetrics.WithLabelValues(lb.Name, lb.Namespace).Set(float64(lb.DeletionTimestamp.Unix()))
	}
}

func RemoveLoadBalancerMetrics(
	lb yawolv1beta1.LoadBalancer,
	metrics *helpermetrics.LoadBalancerMetricList,
) {
	if metrics == nil ||
		metrics.InfoMetrics == nil ||
		metrics.OpenstackInfoMetrics == nil ||
		metrics.ReplicasMetrics == nil ||
		metrics.ReplicasCurrentMetrics == nil ||
		metrics.ReplicasReadyMetrics == nil ||
		metrics.DeletionTimestampMetrics == nil {
		return
	}
	l := map[string]string{
		"lb":        lb.Name,
		"namespace": lb.Namespace,
	}
	metrics.InfoMetrics.DeletePartialMatch(l)
	metrics.OpenstackInfoMetrics.DeletePartialMatch(l)
	metrics.ReplicasMetrics.DeletePartialMatch(l)
	metrics.ReplicasCurrentMetrics.DeletePartialMatch(l)
	metrics.ReplicasReadyMetrics.DeletePartialMatch(l)
	metrics.DeletionTimestampMetrics.DeletePartialMatch(l)
}
