// Package controllers contains reconcile logic for yawollet
package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/go-logr/logr"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/envoystatus"
	"github.com/stackitcloud/yawol/internal/hostmetrics"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	envoycluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyendpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoylistener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoyrbacconfig "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	envoyrbac "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	envoytcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoyudp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	envoyproxyprotocol "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	envoytypev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	envoytypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	envoycache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	envoywellknown "github.com/envoyproxy/go-control-plane/pkg/wellknown"
)

// LoadbalancerCondition condition name for conditions in lbm
type LoadbalancerCondition string

// LoadbalancerConditionStatus condition status for conditions in lbm
type LoadbalancerConditionStatus string

// LoadbalancerMetric metric name for lbm
type LoadbalancerMetric string

// Condition status const
const (
	ConditionTrue  LoadbalancerConditionStatus = "True"
	ConditionFalse LoadbalancerConditionStatus = "False"
)

// Condition name const
const (
	ConfigReady   LoadbalancerCondition = "ConfigReady"
	EnvoyReady    LoadbalancerCondition = "EnvoyReady"
	EnvoyUpToDate LoadbalancerCondition = "EnvoyUpToDate"
)

// Metric name const
const (
	MetricLoad1        LoadbalancerMetric = "load1"
	MetricLoad5        LoadbalancerMetric = "load5"
	MetricLoad15       LoadbalancerMetric = "load15"
	MetricMemTotal     LoadbalancerMetric = "memTotal"
	MetricMemFree      LoadbalancerMetric = "memFree"
	MetricMemAvailable LoadbalancerMetric = "memAvailable"
	MetricStealTime    LoadbalancerMetric = "stealTime"
)

// Envoy health check parameters
const (
	envoyHealthCheckTimeout            int64  = 5
	envoyHealthCheckInterval           int64  = 5
	envoyHealthCheckUnhealthyThreshold uint32 = 3
	envoyHealthCheckHealthyThreshold   uint32 = 2
)

// Supported Protocols
const (
	protocolTCP string = "TCP"
	protocolUDP string = "UDP"
)

// Const declaration for DNS checking
const dnsName string = `^(([a-zA-Z]{1})|([a-zA-Z]{1}[a-zA-Z]{1})|([a-zA-Z]{1}[0-9]{1})|([0-9]{1}[a-zA-Z]{1})|([a-zA-Z0-9][a-zA-Z0-9-_]{1,61}[a-zA-Z0-9])).([a-zA-Z]{2,6}|[a-zA-Z0-9-]{2,30}.[a-zA-Z]{2,3})$` //nolint: lll // long regex
var rxDNSName = regexp.MustCompile(dnsName)

// LoadBalancerReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerReconciler struct {
	client.Client
	Log                     logr.Logger
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	RecorderLB              record.EventRecorder
	LoadbalancerName        string
	LoadbalancerMachineName string
	EnvoyCache              envoycache.SnapshotCache
	ListenAddress           string
	RequeueTime             int
}

// Reconcile handles reconciliation of loadbalancer object
func (r *LoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancer", req.NamespacedName)

	if req.Name != r.LoadbalancerName {
		return ctrl.Result{}, nil
	}

	var lb yawolv1beta1.LoadBalancer
	err := r.Client.Get(ctx, req.NamespacedName, &lb)
	if err != nil {
		return ctrl.Result{}, err
	}
	var lbm yawolv1beta1.LoadBalancerMachine
	err = r.Client.Get(ctx, client.ObjectKey{Name: r.LoadbalancerMachineName, Namespace: req.Namespace}, &lbm)
	if err != nil {
		return ctrl.Result{}, err
	}

	oldSnapshot, err := r.EnvoyCache.GetSnapshot("lb-id")
	if err != nil {
		r.addEvent(&lbm, "Warning", "Error", fmt.Sprintf("Unable to get current snapshot: %v", err))
		return ctrl.Result{}, err
	}

	changed, snapshot, err := r.createEnvoyConfig(oldSnapshot, lb, r.ListenAddress)
	if err != nil {
		r.addEvent(&lbm, "Warning", "Error", fmt.Sprintf("Unable to create new snapshot: %v", err))
		_ = r.updateConditions(ctx, &lbm, ConfigReady, ConditionFalse, "EnvoyConfigurationFailed", "new snapshot cant create successful")
		return ctrl.Result{}, err
	}

	if changed {
		if err = snapshot.Consistent(); err != nil {
			r.addEvent(&lbm, "Warning", "Error", fmt.Sprintf("New snapshot is not consistent: %v", err))
			_ = r.updateConditions(ctx, &lbm, ConfigReady, ConditionFalse, "EnvoyConfigurationFailed", "new snapshot is not Consistent")
			return ctrl.Result{}, err
		}

		err = r.EnvoyCache.SetSnapshot("lb-id", snapshot)
		if err != nil {
			r.addEvent(&lbm, "Warning", "Error", fmt.Sprintf("Unable to set new snapshot: %v", err))
			_ = r.updateConditions(ctx, &lbm, ConfigReady, ConditionFalse, "EnvoyConfigurationFailed", "new snapshot cant set to envoy envoycache")
			return ctrl.Result{}, err
		}
		err = r.updateConditions(ctx, &lbm, ConfigReady, ConditionTrue, "EnvoyConfigurationUpToDate", "envoy config is successfully updated")
		if err != nil {
			return ctrl.Result{}, err
		}
		r.Log.Info("Envoy snapshot updated", "snapshot version", snapshot.GetVersion(resource.ClusterType))
		r.addEvent(&lbm,
			"Normal",
			"Update",
			fmt.Sprintf("Envoy is updated to new snapshot version: %v", snapshot.GetVersion(resource.ClusterType)))
	} else {
		err = r.updateConditions(ctx, &lbm, ConfigReady, ConditionTrue, "EnvoyConfigurationCreated", "envoy config is already up to date")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	envoyStatus := envoystatus.Config{AdminAddress: "127.0.0.1:9000"}

	if envoyStatus.GetEnvoyStatus() {
		err = r.updateConditions(ctx, &lbm, EnvoyReady, ConditionTrue, "EnvoyReady", "envoy response with 200")
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		err = r.updateConditions(ctx, &lbm, EnvoyReady, ConditionFalse, "EnvoyNotReady", "envoy response not with 200")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	envoySnapshotClusterVersion, envoySnapshotListenerVersion, err := envoyStatus.GetCurrentSnapshotVersion()
	// if error in currentSnapshot version => set EnvoyUpToDate condition to False
	// else if version matching  => set EnvoyUpToDate condition to True
	// else => set EnvoyUpToDate condition to False
	if err != nil {
		err = r.updateConditions(ctx, &lbm, EnvoyUpToDate, ConditionFalse, "EnvoySnapshotUpToDate", "unable to get envoy snapshot version")
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if (!changed && envoySnapshotClusterVersion == oldSnapshot.GetVersion(resource.ClusterType) ||
		changed && envoySnapshotClusterVersion == snapshot.GetVersion(resource.ClusterType)) &&
		(!changed && envoySnapshotListenerVersion == oldSnapshot.GetVersion(resource.ListenerType) ||
			changed && envoySnapshotListenerVersion == snapshot.GetVersion(resource.ListenerType)) {
		err = r.updateConditions(ctx, &lbm, EnvoyUpToDate, ConditionTrue, "EnvoySnapshotUpToDate", "envoy snapshot version is up to date")
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		err = r.updateConditions(ctx, &lbm, EnvoyUpToDate, ConditionFalse, "EnvoySnapshotUpToDate", "envoy is not up to date")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	err = r.writeMetrics(ctx, &lbm)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Duration(r.RequeueTime) * time.Second}, nil
}

// SetupWithManager is used by kubebuilder to init the controller loop
func (r *LoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancer{}).
		Complete(r)
}

// updateConditions update a given condition in lbm object
func (r *LoadBalancerReconciler) addEvent(
	lbm *yawolv1beta1.LoadBalancerMachine,
	eventType string,
	eventReason string,
	message string,
) {
	r.Recorder.Event(lbm, eventType, eventReason, message)
}

// updateConditions update a given condition in lbm object
func (r *LoadBalancerReconciler) updateConditions(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
	condition LoadbalancerCondition,
	status LoadbalancerConditionStatus,
	reason string,
	message string,
) error {
	lastTransitionTime := v1.Now()
	var otherConditions string
	if lbm.Status.Conditions != nil {
		for _, c := range *lbm.Status.Conditions {
			if string(c.Type) == string(condition) {
				if string(c.Status) == string(status) {
					lastTransitionTime = c.LastTransitionTime
				}
			} else {
				jsonCondition, err := json.Marshal(c)
				if err != nil {
					return err
				}
				otherConditions += "," + string(jsonCondition)
			}
		}
	}
	patch := []byte(`{ "status": { "conditions": [{` +
		`"lastHeartbeatTime": "` + v1.Now().UTC().Format(time.RFC3339) + `",` +
		`"lastTransitionTime": "` + lastTransitionTime.UTC().Format(time.RFC3339) + `",` +
		`"message": "` + message + `",` +
		`"reason": "` + reason + `",` +
		`"status": "` + string(status) + `",` +
		`"type": "` + string(condition) +
		`"}` +
		otherConditions + `]}}`)
	err := r.Status().Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
	if err != nil {
		return err
	}
	return nil
}

// updateMetrics update metrics in lbm object
func (r *LoadBalancerReconciler) updateMetrics(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
	metric []yawolv1beta1.LoadBalancerMachineMetric,
) error {
	jsonMetrics, err := json.Marshal(metric)
	if err != nil {
		return err
	}
	patch := []byte(`{ "status": { "metrics": ` + string(jsonMetrics) + `}}`)
	err = r.Status().Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
	if err != nil {
		return err
	}
	return nil
}

// createEnvoyConfig create a new envoy snapshot and checks if the new snapshot has changes
func (r *LoadBalancerReconciler) createEnvoyConfig(
	oldSnapshot envoycache.Snapshot,
	lb yawolv1beta1.LoadBalancer,
	listen string,
) (bool, envoycache.Snapshot, error) {
	for _, port := range lb.Spec.Ports {
		if string(port.Protocol) != protocolTCP && string(port.Protocol) != protocolUDP {
			return false, envoycache.Snapshot{}, errors.New("port is not of supported protocol")
		}
		if port.Port > 65535 || port.Port < 1 {
			return false, envoycache.Snapshot{}, errors.New("port is not between 1 and 65535")
		}
		if port.NodePort > 65535 || port.NodePort < 1 {
			return false, envoycache.Snapshot{}, errors.New("NodePort is not between 1 and 65535")
		}
	}

	for _, endpoints := range lb.Spec.Endpoints {
		if endpoints.Addresses == nil {
			return false, envoycache.Snapshot{}, errors.New("addresses are nil")
		}
		for _, addresses := range endpoints.Addresses {
			if !rxDNSName.MatchString(addresses) && net.ParseIP(addresses) == nil {
				return false, envoycache.Snapshot{}, errors.New("wrong endpoint address format (DNS name) not correct")
			}
		}
	}

	versionInt, err := strconv.Atoi(oldSnapshot.GetVersion(resource.ListenerType))
	if err != nil {
		versionInt = 0
	}
	newVersion := fmt.Sprintf("%v", versionInt+1)
	newSnapshot := envoycache.NewSnapshot(
		newVersion,
		nil, // endpoints
		r.createClusters(lb),
		nil,
		r.createListener(lb, listen),
		nil, // runtimes
		nil, // secrets
	)

	if fmt.Sprint(newSnapshot.GetResources(resource.ClusterType)) == fmt.Sprint(oldSnapshot.GetResources(resource.ClusterType)) &&
		fmt.Sprint(newSnapshot.GetResources(resource.ListenerType)) == fmt.Sprint(oldSnapshot.GetResources(resource.ListenerType)) {
		// nothing to change
		return false, envoycache.Snapshot{}, nil
	}
	// return new snapshot
	return true, newSnapshot, nil
}

// createClusters create clusters for envoy snapshot
func (r *LoadBalancerReconciler) createClusters(lb yawolv1beta1.LoadBalancer) []envoytypes.Resource {
	clusters := make([]envoytypes.Resource, len(lb.Spec.Ports))
	for i, port := range lb.Spec.Ports {
		var protocol envoycore.SocketAddress_Protocol
		var healthChecks []*envoycore.HealthCheck
		var transportSocket *envoycore.TransportSocket

		if string(port.Protocol) == protocolTCP {
			protocol = envoycore.SocketAddress_TCP
			healthChecks = []*envoycore.HealthCheck{{
				Timeout:            &duration.Duration{Seconds: envoyHealthCheckTimeout},
				Interval:           &duration.Duration{Seconds: envoyHealthCheckInterval},
				UnhealthyThreshold: &wrappers.UInt32Value{Value: envoyHealthCheckUnhealthyThreshold},
				HealthyThreshold:   &wrappers.UInt32Value{Value: envoyHealthCheckHealthyThreshold},
				HealthChecker: &envoycore.HealthCheck_TcpHealthCheck_{
					TcpHealthCheck: &envoycore.HealthCheck_TcpHealthCheck{
						Send:    nil,
						Receive: []*envoycore.HealthCheck_Payload{},
					}},
			}}

			if lb.Spec.Options.TCPProxyProtocol {
				if config, err := anypb.New(&envoyproxyprotocol.ProxyProtocolUpstreamTransport{
					Config: &envoycore.ProxyProtocolConfig{Version: 2},
					TransportSocket: &envoycore.TransportSocket{
						Name: envoywellknown.TransportSocketRawBuffer,
					},
				}); err == nil {
					transportSocket = &envoycore.TransportSocket{
						// TODO constant is not in envoy.wellknown
						Name: "envoy.transport_sockets.upstream_proxy_protocol",
						ConfigType: &envoycore.TransportSocket_TypedConfig{
							TypedConfig: config,
						},
					}
				}
			}
		} else if string(port.Protocol) == protocolUDP {
			protocol = envoycore.SocketAddress_UDP
			// health checks are only implemented for TCP
		}

		endpoints := make([]*envoyendpoint.LocalityLbEndpoints, len(lb.Spec.Endpoints))
		for iEndpoint, endpointSpec := range lb.Spec.Endpoints {
			addressEndpoints := make([]*envoyendpoint.LbEndpoint, len(endpointSpec.Addresses))
			for iAddresses, address := range endpointSpec.Addresses {
				addressEndpoints[iAddresses] = &envoyendpoint.LbEndpoint{
					HostIdentifier: &envoyendpoint.LbEndpoint_Endpoint{
						Endpoint: &envoyendpoint.Endpoint{
							Address: &envoycore.Address{
								Address: &envoycore.Address_SocketAddress{
									SocketAddress: &envoycore.SocketAddress{
										Protocol: protocol,
										Address:  address,
										PortSpecifier: &envoycore.SocketAddress_PortValue{
											PortValue: uint32(port.NodePort),
										},
									},
								},
							},
						},
					},
				}
			}

			endpoints[iEndpoint] = &envoyendpoint.LocalityLbEndpoints{
				LbEndpoints: addressEndpoints,
			}
		}
		clusterPort := &envoycluster.Cluster{
			Name:                 fmt.Sprintf("%v-%v", port.Protocol, port.Port),
			ConnectTimeout:       &duration.Duration{Seconds: 5},
			ClusterDiscoveryType: &envoycluster.Cluster_Type{Type: envoycluster.Cluster_STATIC},
			CommonLbConfig: &envoycluster.Cluster_CommonLbConfig{
				HealthyPanicThreshold: &envoytypev3.Percent{Value: 0},
			},
			LbPolicy: envoycluster.Cluster_ROUND_ROBIN,
			LoadAssignment: &envoyendpoint.ClusterLoadAssignment{
				ClusterName: fmt.Sprintf("%v-%v", port.Protocol, port.Port),
				Endpoints:   endpoints,
			},
			DnsLookupFamily:               envoycluster.Cluster_V4_ONLY,
			HealthChecks:                  healthChecks,
			TransportSocket:               transportSocket,
			PerConnectionBufferLimitBytes: &wrappers.UInt32Value{Value: 32768}, // 32Kib
			CircuitBreakers: &envoycluster.CircuitBreakers{
				Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
					{
						Priority:           envoycore.RoutingPriority_DEFAULT,
						MaxConnections:     &wrappers.UInt32Value{Value: 10000 * uint32(hostmetrics.GetCPUNum())},
						MaxRequests:        &wrappers.UInt32Value{Value: 8000 * uint32(hostmetrics.GetCPUNum())},
						MaxPendingRequests: &wrappers.UInt32Value{Value: 2000 * uint32(hostmetrics.GetCPUNum())},
					},
				},
			},
		}
		clusters[i] = clusterPort
	}

	return clusters
}

// createListener create envoylistener for envoy snapshot
func (r *LoadBalancerReconciler) createListener(
	lb yawolv1beta1.LoadBalancer,
	listenAddress string,
) []envoytypes.Resource {
	listeners := make([]envoytypes.Resource, len(lb.Spec.Ports))
	for i, port := range lb.Spec.Ports {
		// unsupported protocol is already checked earlier
		if string(port.Protocol) == protocolTCP {
			listeners[i] = r.createTCPListener(lb, listenAddress, port)
		} else if string(port.Protocol) == protocolUDP {
			listeners[i] = r.createUDPListener(lb, listenAddress, port)
		}
	}

	return listeners
}

func (r *LoadBalancerReconciler) createRBACRules(
	lb yawolv1beta1.LoadBalancer,
) *anypb.Any {
	principals := []*envoyrbacconfig.Principal{}
	for _, sourceRange := range lb.Spec.LoadBalancerSourceRanges {
		// validate CIDR and ignore if invalid
		_, _, err := net.ParseCIDR(sourceRange)
		split := strings.Split(sourceRange, "/")

		errorMessage := "Could not parse LoadBalancerSourceRange: " + sourceRange
		if err != nil || len(split) != 2 {
			r.RecorderLB.Event(&lb, "Warning", "Error", errorMessage)
			continue
		}

		prefix, err := strconv.ParseUint(split[1], 10, 32)
		if err != nil {
			r.RecorderLB.Event(&lb, "Warning", "Error", errorMessage)
			continue
		}

		principals = append(principals, &envoyrbacconfig.Principal{
			Identifier: &envoyrbacconfig.Principal_DirectRemoteIp{
				DirectRemoteIp: &envoycore.CidrRange{
					AddressPrefix: split[0],
					PrefixLen:     &wrappers.UInt32Value{Value: uint32(prefix)},
				},
			},
		})
	}

	rules, err := anypb.New(&envoyrbac.RBAC{
		StatPrefix: "envoyrbac",
		Rules: &envoyrbacconfig.RBAC{
			Action: envoyrbacconfig.RBAC_ALLOW,
			Policies: map[string]*envoyrbacconfig.Policy{
				"load-balancer-source-ranges": {
					Permissions: []*envoyrbacconfig.Permission{{
						// allow everything if principal matches
						Rule: &envoyrbacconfig.Permission_Any{Any: true},
					}},
					Principals: principals,
				},
			},
		},
	})

	if err != nil {
		r.RecorderLB.Event(&lb, "Warning", "Error", "Could not parse LoadBalancerSourceRange, no rules applied")
		return nil
	}

	return rules
}

func (r *LoadBalancerReconciler) createTCPListener(
	lb yawolv1beta1.LoadBalancer,
	listenAddress string,
	port corev1.ServicePort,
) *envoylistener.Listener {
	listenPort, err := anypb.New(&envoytcp.TcpProxy{
		StatPrefix:       "envoytcp",
		ClusterSpecifier: &envoytcp.TcpProxy_Cluster{Cluster: fmt.Sprintf("%v-%v", port.Protocol, port.Port)},
	})

	if err != nil {
		panic(err)
	}

	filters := []*envoylistener.Filter{}

	// ip whitelisting via RBAC according to loadBalancerSourceRanges
	if lb.Spec.LoadBalancerSourceRanges != nil && len(lb.Spec.LoadBalancerSourceRanges) > 0 {
		lbSourceRangeFilter := r.createRBACRules(lb)
		if lbSourceRangeFilter != nil {
			filters = append(filters, &envoylistener.Filter{
				Name: envoywellknown.RoleBasedAccessControl,
				ConfigType: &envoylistener.Filter_TypedConfig{
					TypedConfig: lbSourceRangeFilter,
				},
			})
		}
	}

	return &envoylistener.Listener{
		Name: fmt.Sprintf("%v-%v", port.Protocol, port.Port),
		Address: &envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					Protocol: envoycore.SocketAddress_TCP,
					Address:  listenAddress,
					PortSpecifier: &envoycore.SocketAddress_PortValue{
						PortValue: uint32(port.Port),
					},
				},
			},
		},
		FilterChains: []*envoylistener.FilterChain{{
			// proxy filter has to be the last in the chain
			Filters: append(filters, &envoylistener.Filter{
				Name: envoywellknown.TCPProxy,
				ConfigType: &envoylistener.Filter_TypedConfig{
					TypedConfig: listenPort,
				},
			}),
		}},
		Freebind:                      &wrappers.BoolValue{Value: true},
		ReusePort:                     true,
		PerConnectionBufferLimitBytes: &wrappers.UInt32Value{Value: 32768}, // 32 Kib
	}
}

func (r *LoadBalancerReconciler) createUDPListener(
	// nolint: unparam // will ber needed for rbac rules in the future
	lb yawolv1beta1.LoadBalancer,
	listenAddress string,
	port corev1.ServicePort,
) *envoylistener.Listener {
	listenPort, err := anypb.New(&envoyudp.UdpProxyConfig{
		StatPrefix:     "envoyudp",
		RouteSpecifier: &envoyudp.UdpProxyConfig_Cluster{Cluster: fmt.Sprintf("%v-%v", port.Protocol, port.Port)},
	})
	if err != nil {
		panic(err)
	}

	// TODO UDP only supports a single filter at the moment
	// ref: https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener.proto -> listener_filters
	// -> load-balancer-source-ranges are only guaranteed via OpenStack
	return &envoylistener.Listener{
		Name: fmt.Sprintf("%v-%v", port.Protocol, port.Port),
		Address: &envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					Protocol: envoycore.SocketAddress_UDP,
					Address:  listenAddress,
					PortSpecifier: &envoycore.SocketAddress_PortValue{
						PortValue: uint32(port.Port),
					},
				},
			},
		},
		ListenerFilters: []*envoylistener.ListenerFilter{
			{
				// TODO this is not in envoywellknown
				Name: "envoy.filters.udp_listener.udp_proxy",
				ConfigType: &envoylistener.ListenerFilter_TypedConfig{
					TypedConfig: listenPort,
				},
			},
		},
		Freebind:                      &wrappers.BoolValue{Value: true},
		ReusePort:                     true,
		PerConnectionBufferLimitBytes: &wrappers.UInt32Value{Value: 32768}, // 32 Kib
	}
}

// writeMetrics gets metrics and write new metrics to lbm
func (r *LoadBalancerReconciler) writeMetrics(ctx context.Context, lbm *yawolv1beta1.LoadBalancerMachine) error {
	load1, load5, load15, err := hostmetrics.GetLoad()
	if err != nil {
		return err
	}

	memTotal, memFree, memAvailable, err := hostmetrics.GetMem()
	if err != nil {
		return err
	}

	stealTime, err := hostmetrics.GetCPUStealTime()
	if err != nil {
		return err
	}

	metrics := []yawolv1beta1.LoadBalancerMachineMetric{
		{
			Type:  string(MetricLoad1),
			Value: load1,
			Time:  v1.Now(),
		}, {
			Type:  string(MetricLoad5),
			Value: load5,
			Time:  v1.Now(),
		}, {
			Type:  string(MetricLoad15),
			Value: load15,
			Time:  v1.Now(),
		}, {
			Type:  string(MetricMemTotal),
			Value: memTotal,
			Time:  v1.Now(),
		}, {
			Type:  string(MetricMemFree),
			Value: memFree,
			Time:  v1.Now(),
		}, {
			Type:  string(MetricMemAvailable),
			Value: memAvailable,
			Time:  v1.Now(),
		}, {
			Type:  string(MetricStealTime),
			Value: stealTime,
			Time:  v1.Now(),
		},
	}
	envoyStatus := envoystatus.Config{AdminAddress: "127.0.0.1:9000"}
	var envoyMetrics []yawolv1beta1.LoadBalancerMachineMetric
	envoyMetrics, err = envoyStatus.GetCurrentStats()
	if err != nil {
		return err
	}
	metrics = append(metrics, envoyMetrics...)

	err = r.updateMetrics(ctx, lbm, metrics)
	if err != nil {
		return err
	}

	return nil
}
