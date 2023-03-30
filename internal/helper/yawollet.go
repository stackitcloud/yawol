package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shirou/gopsutil/v3/process"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/envoystatus"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"
	"github.com/stackitcloud/yawol/internal/hostmetrics"
	"github.com/stackitcloud/yawol/internal/keepalived"

	xdscore "github.com/cncf/xds/go/xds/core/v3"
	xdsmatcher "github.com/cncf/xds/go/xds/type/matcher/v3"

	envoycluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyendpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoylistener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoyrbacconfig "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	envoyrbac "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	envoytcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoyudp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	envoyproxyprotocol "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	envoyrawbuffer "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	envoytypev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	envoytypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	envoycache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	envoywellknown "github.com/envoyproxy/go-control-plane/pkg/wellknown"
)

// LoadbalancerCondition condition name for conditions in lbm
type LoadbalancerCondition string

// LoadbalancerConditionStatus condition status for conditions in lbm
type LoadbalancerConditionStatus string

// LoadbalancerMetric metric name for lbm
type LoadbalancerMetric string

// TODO: replace with constants from envoywellknown when available
const (
	FilterUDPProxy string = "envoy.filters.udp_listener.udp_proxy"
)

// Condition status const
const (
	ConditionTrue  LoadbalancerConditionStatus = "True"
	ConditionFalse LoadbalancerConditionStatus = "False"
)

// Condition name const
const (
	ConfigReady         LoadbalancerCondition = "ConfigReady"
	EnvoyReady          LoadbalancerCondition = "EnvoyReady"
	EnvoyUpToDate       LoadbalancerCondition = "EnvoyUpToDate"
	KeepalivedProcess   LoadbalancerCondition = "KeepalivedProcess"
	KeepalivedStatsFile LoadbalancerCondition = "KeepalivedStatsFile"
	KeepalivedMaster    LoadbalancerCondition = "KeepalivedMaster"
)

// Metric name const
const (
	MetricLoad1                            LoadbalancerMetric = "load1"
	MetricLoad5                            LoadbalancerMetric = "load5"
	MetricLoad15                           LoadbalancerMetric = "load15"
	MetricUptime                           LoadbalancerMetric = "uptime"
	MetricNumCPU                           LoadbalancerMetric = "numCPU"
	MetricMemTotal                         LoadbalancerMetric = "memTotal"
	MetricMemFree                          LoadbalancerMetric = "memFree"
	MetricMemAvailable                     LoadbalancerMetric = "memAvailable"
	MetricStealTime                        LoadbalancerMetric = "stealTime"
	MetricKeepalivedIsMaster               LoadbalancerMetric = "keepalivedIsMaster"
	MetricKeepalivedBecameMaster           LoadbalancerMetric = "keepalivedBecameMaster"
	MetricKeepalivedReleasedMaster         LoadbalancerMetric = "keepalivedReleasedMaster"
	MetricKeepalivedAdvertisementsSent     LoadbalancerMetric = "keepalivedAdvertisementsSent"
	MetricKeepalivedAdvertisementsReceived LoadbalancerMetric = "keepalivedAdvertisementsReceived"
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
const dnsName string = `^(([a-zA-Z]{1})|([a-zA-Z]{1}[a-zA-Z]{1})|([a-zA-Z]{1}[0-9]{1})|([0-9]{1}[a-zA-Z]{1})|([a-zA-Z0-9][a-zA-Z0-9-_]{1,61}[a-zA-Z0-9])).([a-zA-Z]{2,6}|[a-zA-Z0-9-]{2,30}.[a-zA-Z]{2,3})$` //nolint:lll // long regex
var rxDNSName = regexp.MustCompile(dnsName)

// Const declaration for SSH pub key checking
const sshPublicKey = `^ssh-rsa\s+[A-Za-z0-9+/=]+$`

var rxSSHPublicKey = regexp.MustCompile(sshPublicKey)

// CreateEnvoyConfig create a new envoy snapshot and checks if the new snapshot has changes
func CreateEnvoyConfig(
	r record.EventRecorder,
	oldSnapshot envoycache.ResourceSnapshot,
	lb *yawolv1beta1.LoadBalancer,
	listen string,
) (bool, envoycache.Snapshot, error) {
	for _, port := range lb.Spec.Ports {
		if string(port.Protocol) != protocolTCP && string(port.Protocol) != protocolUDP {
			return false, envoycache.Snapshot{}, ErrPortProtocolNotSupported
		}
		if port.Port > 65535 || port.Port < 1 {
			return false, envoycache.Snapshot{}, ErrPortInvalidRange
		}
		if port.NodePort > 65535 || port.NodePort < 1 {
			return false, envoycache.Snapshot{}, ErrNodePortInvalidRange
		}
	}

	for _, endpoints := range lb.Spec.Endpoints {
		if endpoints.Addresses == nil {
			return false, envoycache.Snapshot{}, ErrEndpointAddressesNil
		}
		for _, addresses := range endpoints.Addresses {
			if !rxDNSName.MatchString(addresses) && net.ParseIP(addresses) == nil {
				return false, envoycache.Snapshot{}, ErrEndpointAddressWrongFormat
			}
		}
	}

	versionInt, err := strconv.Atoi(oldSnapshot.GetVersion(resource.ListenerType))
	if err != nil {
		versionInt = 0
	}
	newVersion := fmt.Sprintf("%v", versionInt+1)

	newSnapshot, err := envoycache.NewSnapshot(newVersion, map[resource.Type][]envoytypes.Resource{
		resource.EndpointType: {},
		resource.ClusterType:  createEnvoyCluster(lb),
		resource.RouteType:    nil,
		resource.ListenerType: createEnvoyListener(r, lb, listen),
		resource.RuntimeType:  nil,
		resource.SecretType:   nil,
	})
	if err != nil {
		return false, envoycache.Snapshot{}, err
	}

	if fmt.Sprint(newSnapshot.GetResources(resource.ClusterType)) == fmt.Sprint(oldSnapshot.GetResources(resource.ClusterType)) &&
		fmt.Sprint(newSnapshot.GetResources(resource.ListenerType)) == fmt.Sprint(oldSnapshot.GetResources(resource.ListenerType)) {
		// nothing to change
		return false, envoycache.Snapshot{}, nil
	}

	// return new snapshot
	return true, *newSnapshot, nil
}

func createEnvoyCluster(lb *yawolv1beta1.LoadBalancer) []envoytypes.Resource {
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

			if proxyProtocolEnabled(lb.Spec.Options, port) {
				// NOTE: the nested ifs are not optimal but panicing doesn't seem optimal either
				// TODO: use logging on error
				if rawConfig, err := anypb.New(&envoyrawbuffer.RawBuffer{}); err == nil {
					if proxyConfig, err := anypb.New(&envoyproxyprotocol.ProxyProtocolUpstreamTransport{
						Config: &envoycore.ProxyProtocolConfig{Version: 2},
						TransportSocket: &envoycore.TransportSocket{
							Name: envoywellknown.TransportSocketRawBuffer,
							ConfigType: &envoycore.TransportSocket_TypedConfig{
								TypedConfig: rawConfig,
							},
						},
					}); err == nil {
						transportSocket = &envoycore.TransportSocket{
							// TODO: constant is not in envoy.wellknown
							Name: "envoy.transport_sockets.upstream_proxy_protocol",
							ConfigType: &envoycore.TransportSocket_TypedConfig{
								TypedConfig: proxyConfig,
							},
						}
					}
				}
			}
		} else if string(port.Protocol) == protocolUDP {
			protocol = envoycore.SocketAddress_UDP
			// health checks are only implemented for TCP
			// proxy protocol only works for TCP
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
			DnsLookupFamily:               envoycluster.Cluster_AUTO,
			HealthChecks:                  healthChecks,
			TransportSocket:               transportSocket,
			PerConnectionBufferLimitBytes: wrapperspb.UInt32(32 * 1024),
			CircuitBreakers: &envoycluster.CircuitBreakers{
				Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
					{
						Priority:           envoycore.RoutingPriority_DEFAULT,
						MaxConnections:     wrapperspb.UInt32(uint32(10000 * hostmetrics.GetCPUNum())),
						MaxRequests:        wrapperspb.UInt32(uint32(8000 * hostmetrics.GetCPUNum())),
						MaxPendingRequests: wrapperspb.UInt32(uint32(2000 * hostmetrics.GetCPUNum())),
					},
				},
			},
		}
		clusters[i] = clusterPort
	}

	return clusters
}

// createEnvoyListener create envoylistener for envoy snapshot
func createEnvoyListener(
	r record.EventRecorder,
	lb *yawolv1beta1.LoadBalancer,
	listenAddress string,
) []envoytypes.Resource {
	listeners := make([]envoytypes.Resource, len(lb.Spec.Ports))
	for i, port := range lb.Spec.Ports {
		// unsupported protocol is already checked earlier
		if string(port.Protocol) == protocolTCP {
			listeners[i] = createEnvoyTCPListener(r, lb, listenAddress, port)
		} else if string(port.Protocol) == protocolUDP {
			listeners[i] = createEnvoyUDPListener(lb, listenAddress, port)
		}
	}

	return listeners
}

func createEnvoyTCPListener(
	r record.EventRecorder,
	lb *yawolv1beta1.LoadBalancer,
	listenAddress string,
	port corev1.ServicePort,
) *envoylistener.Listener {
	var idleTimeout *duration.Duration

	if lb.Spec.Options.TCPIdleTimeout != nil {
		idleTimeout = durationpb.New(lb.Spec.Options.TCPIdleTimeout.Duration)
	}

	listenPort, err := anypb.New(&envoytcp.TcpProxy{
		StatPrefix:       "envoytcp",
		ClusterSpecifier: &envoytcp.TcpProxy_Cluster{Cluster: fmt.Sprintf("%v-%v", port.Protocol, port.Port)},
		IdleTimeout:      idleTimeout,
	})

	if err != nil {
		panic(err)
	}

	var filters []*envoylistener.Filter

	// ip whitelisting via RBAC according to loadBalancerSourceRanges
	if lb.Spec.Options.LoadBalancerSourceRanges != nil && len(lb.Spec.Options.LoadBalancerSourceRanges) > 0 {
		lbSourceRangeFilter := createEnvoyRBACRules(r, lb)
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
			Filters: append(filters, &envoylistener.Filter{
				// proxy filter has to be the last in the chain
				Name: envoywellknown.TCPProxy,
				ConfigType: &envoylistener.Filter_TypedConfig{
					TypedConfig: listenPort,
				},
			}),
		}},
		Freebind:                      wrapperspb.Bool(true),
		EnableReusePort:               wrapperspb.Bool(true),
		PerConnectionBufferLimitBytes: wrapperspb.UInt32(32 * 1024),
	}
}

func createEnvoyUDPListener(
	lb *yawolv1beta1.LoadBalancer,
	listenAddress string,
	port corev1.ServicePort,
) *envoylistener.Listener {
	routeConfig, err := anypb.New(&envoyudp.Route{
		Cluster: fmt.Sprintf("%v-%v", port.Protocol, port.Port),
	})
	if err != nil {
		panic(err)
	}

	var idleTimeout *duration.Duration
	if lb.Spec.Options.UDPIdleTimeout != nil {
		idleTimeout = durationpb.New(lb.Spec.Options.UDPIdleTimeout.Duration)
	}

	listenPort, err := anypb.New(&envoyudp.UdpProxyConfig{
		StatPrefix:  "envoyudp",
		IdleTimeout: idleTimeout,
		RouteSpecifier: &envoyudp.UdpProxyConfig_Matcher{
			Matcher: &xdsmatcher.Matcher{
				OnNoMatch: &xdsmatcher.Matcher_OnMatch{
					OnMatch: &xdsmatcher.Matcher_OnMatch_Action{
						Action: &xdscore.TypedExtensionConfig{
							Name:        "route",
							TypedConfig: routeConfig,
						},
					},
				},
			},
		},
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
				Name: FilterUDPProxy,
				ConfigType: &envoylistener.ListenerFilter_TypedConfig{
					TypedConfig: listenPort,
				},
			},
		},
		Freebind:                      wrapperspb.Bool(true),
		EnableReusePort:               wrapperspb.Bool(true),
		PerConnectionBufferLimitBytes: wrapperspb.UInt32(32 * 1024),
	}
}

func GetRoleRules(
	loadBalancer *yawolv1beta1.LoadBalancer,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
) []rbac.PolicyRule {
	return []rbac.PolicyRule{{
		Verbs:     []string{"create"},
		APIGroups: []string{""},
		Resources: []string{"events"},
	}, {
		Verbs:     []string{"list", "watch"},
		APIGroups: []string{"yawol.stackit.cloud"},
		Resources: []string{"loadbalancers"},
	}, {
		Verbs:         []string{"get"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancers"},
		ResourceNames: []string{loadBalancer.Name},
	}, {
		Verbs:     []string{"list", "watch"},
		APIGroups: []string{"yawol.stackit.cloud"},
		Resources: []string{"loadbalancermachines"},
	}, {
		Verbs:         []string{"get", "update", "patch"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancermachines"},
		ResourceNames: []string{loadBalancerMachine.Name},
	}, {
		Verbs:         []string{"get", "update", "patch"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancermachines/status"},
		ResourceNames: []string{loadBalancerMachine.Name},
	}}
}

func createEnvoyRBACRules(
	r record.EventRecorder,
	lb *yawolv1beta1.LoadBalancer,
) *anypb.Any {
	principals := []*envoyrbacconfig.Principal{}
	for _, sourceRange := range lb.Spec.Options.LoadBalancerSourceRanges {
		// validate CIDR and ignore if invalid
		_, _, err := net.ParseCIDR(sourceRange)
		split := strings.Split(sourceRange, "/")

		if err != nil || len(split) != 2 {
			_ = kubernetes.SendErrorAsEvent(r, fmt.Errorf("%w: SourceRange: %s", ErrCouldNotParseSourceRange, sourceRange), lb)
			continue
		}

		prefix, err := strconv.ParseUint(split[1], 10, 32)
		if err != nil {
			_ = kubernetes.SendErrorAsEvent(r, fmt.Errorf("%w: SourceRange: %s", ErrCouldNotParseSourceRange, sourceRange), lb)
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
		_ = kubernetes.SendErrorAsEvent(r, fmt.Errorf("%w: no rule applied", ErrCouldNotParseSourceRange), lb)
		return nil
	}

	return rules
}

// proxyProtocolEnabled returns true if TCPProxyProtocol should be enabled for the given port
func proxyProtocolEnabled(options yawolv1beta1.LoadBalancerOptions, port corev1.ServicePort) bool {
	if !options.TCPProxyProtocol {
		// TCPProxyProtocol is disabled
		return false
	}

	if len(options.TCPProxyProtocolPortsFilter) == 0 {
		// TCPProxyProtocol is enabled and no filter is set
		return true
	}

	for _, portInFilter := range options.TCPProxyProtocolPortsFilter {
		if portInFilter == port.Port {
			// TCPProxyProtocol is enabled and port is found in filter list
			return true
		}
	}

	// TCPProxyProtocol is enabled and port is not found in filter list
	return false
}

// UpdateLBMConditions update a given condition in lbm object
func UpdateLBMConditions(
	ctx context.Context,
	c client.StatusWriter,
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
	err := c.Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
	if err != nil {
		return err
	}
	return nil
}

// WriteLBMMetrics gets metrics and write new metrics to lbm
func WriteLBMMetrics(
	ctx context.Context,
	c client.StatusWriter,
	keepalivedStatsFile string,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	metrics := []yawolv1beta1.LoadBalancerMachineMetric{}
	if load1, load5, load15, err := hostmetrics.GetLoad(); err == nil {
		metrics = append(metrics, []yawolv1beta1.LoadBalancerMachineMetric{
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
			}}...)
	}

	if uptime, err := hostmetrics.GetUptime(); err == nil {
		metrics = append(metrics, yawolv1beta1.LoadBalancerMachineMetric{
			Type:  string(MetricUptime),
			Value: uptime,
			Time:  v1.Now(),
		})
	}

	if memTotal, memFree, memAvailable, err := hostmetrics.GetMem(); err == nil {
		metrics = append(metrics, []yawolv1beta1.LoadBalancerMachineMetric{
			{
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
			}}...)
	}

	if stealTime, err := hostmetrics.GetCPUStealTime(); err == nil {
		metrics = append(metrics, yawolv1beta1.LoadBalancerMachineMetric{
			Type:  string(MetricStealTime),
			Value: stealTime,
			Time:  v1.Now(),
		})
	}

	metrics = append(metrics, yawolv1beta1.LoadBalancerMachineMetric{
		Type:  string(MetricNumCPU),
		Value: strconv.Itoa(hostmetrics.GetCPUNum()),
		Time:  v1.Now(),
	})

	envoyStatus := envoystatus.Config{AdminAddress: "127.0.0.1:9000"}
	if envoyMetrics, err := envoyStatus.GetCurrentStats(); err == nil {
		metrics = append(metrics, envoyMetrics...)
	}

	if keepalivedStatsFile != "" {
		if keepalivedStats, _, err := keepalived.ReadStatsForInstanceName(VRRPInstanceName, keepalivedStatsFile); err == nil {
			masterInt := "0"
			if keepalivedStats.IsMaster() {
				masterInt = "1"
			}
			metrics = append(metrics, []yawolv1beta1.LoadBalancerMachineMetric{
				{
					Type:  string(MetricKeepalivedIsMaster),
					Value: masterInt,
					Time:  v1.Now(),
				}, {
					Type:  string(MetricKeepalivedReleasedMaster),
					Value: strconv.Itoa(keepalivedStats.ReleasedMaster),
					Time:  v1.Now(),
				}, {
					Type:  string(MetricKeepalivedBecameMaster),
					Value: strconv.Itoa(keepalivedStats.BecameMaster),
					Time:  v1.Now(),
				}, {
					Type:  string(MetricKeepalivedAdvertisementsSent),
					Value: strconv.Itoa(keepalivedStats.Advertisements.Sent),
					Time:  v1.Now(),
				}, {
					Type:  string(MetricKeepalivedAdvertisementsReceived),
					Value: strconv.Itoa(keepalivedStats.Advertisements.Received),
					Time:  v1.Now(),
				}}...)
		}
	}

	return updateLBMMetrics(ctx, c, lbm, metrics)
}

// updateLBMMetrics update metrics in lbm object
func updateLBMMetrics(
	ctx context.Context,
	c client.StatusWriter,
	lbm *yawolv1beta1.LoadBalancerMachine,
	metric []yawolv1beta1.LoadBalancerMachineMetric,
) error {
	jsonMetrics, err := json.Marshal(metric)
	if err != nil {
		return err
	}
	patch := []byte(`{ "status": { "metrics": ` + string(jsonMetrics) + `}}`)
	err = c.Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
	if err != nil {
		return err
	}
	return nil
}

func CheckEnvoyVersion(
	ctx context.Context,
	c client.StatusWriter,
	lbm *yawolv1beta1.LoadBalancerMachine,
	snapshot envoycache.ResourceSnapshot,
) error {
	envoyStatus := envoystatus.Config{AdminAddress: "127.0.0.1:9000"}

	envoySnapshotClusterVersion, envoySnapshotListenerVersion, err := envoyStatus.GetCurrentSnapshotVersion()
	if err != nil {
		return UpdateLBMConditions(ctx, c, lbm, EnvoyUpToDate, ConditionFalse, "EnvoySnapshotUpToDate", "unable to get envoy snapshot version")
	}

	if envoySnapshotClusterVersion == snapshot.GetVersion(resource.ClusterType) &&
		envoySnapshotListenerVersion == snapshot.GetVersion(resource.ListenerType) {
		return UpdateLBMConditions(ctx, c, lbm, EnvoyUpToDate, ConditionTrue, "EnvoySnapshotUpToDate", "envoy snapshot version is up to date")
	}
	return UpdateLBMConditions(ctx, c, lbm, EnvoyUpToDate, ConditionFalse, "EnvoySnapshotUpToDate", "envoy is not up to date")
}

func UpdateEnvoyStatus(
	ctx context.Context,
	c client.StatusWriter,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	envoyStatus := envoystatus.Config{AdminAddress: "127.0.0.1:9000"}
	if envoyStatus.GetEnvoyStatus() {
		return UpdateLBMConditions(ctx, c, lbm, EnvoyReady, ConditionTrue, "EnvoyReady", "envoy response with 200")
	}
	return UpdateLBMConditions(ctx, c, lbm, EnvoyReady, ConditionFalse, "EnvoyNotReady", "envoy response not with 200")
}

func UpdateKeepalivedStatus(
	ctx context.Context,
	c client.StatusWriter,
	keepalivedStatsFile string,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	err := UpdateKeepalivedStatsStatus(ctx, c, keepalivedStatsFile, lbm)
	if err != nil {
		return err
	}

	err = UpdateKeepalivedPIDStatus(ctx, c, lbm)
	if err != nil {
		return err
	}

	return nil
}

func UpdateKeepalivedStatsStatus(
	ctx context.Context,
	c client.StatusWriter,
	keepalivedStatsFile string,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	if keepalivedStatsFile == "" {
		return nil
	}

	keepalivedStats, modTime, err := keepalived.ReadStatsForInstanceName(VRRPInstanceName, keepalivedStatsFile)
	if err != nil {
		if err := UpdateLBMConditions(ctx, c, lbm,
			KeepalivedStatsFile,
			ConditionFalse,
			"CouldNotReadStats", "Could not get stats file"); err != nil {
			return err
		}
		if err := UpdateLBMConditions(ctx, c, lbm,
			KeepalivedMaster, ConditionFalse,
			"CouldNotReadStats", "Could not get stats file"); err != nil {
			return err
		}
		return nil
	}

	keepalivedIsMaster := ConditionFalse
	if keepalivedStats.IsMaster() {
		keepalivedIsMaster = ConditionTrue
	}
	if err := UpdateLBMConditions(ctx, c, lbm,
		KeepalivedMaster,
		keepalivedIsMaster,
		"KeepalivedStatus", "Read master status from stats file"); err != nil {
		return err
	}

	if modTime.Before(time.Now().Add(-5 * time.Minute)) {
		return UpdateLBMConditions(ctx, c, lbm,
			KeepalivedStatsFile,
			ConditionFalse,
			"StatsNotUpToDate", "Keepalived stat file is older than 5 min")
	}

	return UpdateLBMConditions(ctx, c, lbm,
		KeepalivedStatsFile,
		ConditionTrue,
		"StatsUpToDate", "Keepalived stat file is newer than 5 min")
}

func UpdateKeepalivedPIDStatus(
	ctx context.Context,
	c client.StatusWriter,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	pidFile, err := os.ReadFile("/run/keepalived.pid")
	if err != nil {
		return UpdateLBMConditions(ctx, c, lbm,
			KeepalivedProcess,
			ConditionFalse,
			"CouldNotGetPIDFile", "Could not get pid file: "+err.Error())
	}

	pidID, err := strconv.Atoi(string(pidFile))
	if err != nil {
		return UpdateLBMConditions(ctx, c, lbm,
			KeepalivedProcess,
			ConditionFalse,
			"CouldNotGetPID", "Could not get pid: "+err.Error())
	}

	keepalivedProc, err := process.NewProcess(int32(pidID))
	if err != nil {
		return UpdateLBMConditions(ctx, c, lbm,
			KeepalivedProcess,
			ConditionFalse,
			"CouldNotKeepalivedProcess", "Could not get keepalived process: "+err.Error())
	}
	keepalivedRunning, err := keepalivedProc.IsRunning()
	if err != nil {
		return UpdateLBMConditions(ctx, c, lbm,
			KeepalivedProcess,
			ConditionFalse,
			"CouldNotKeepalivedProcess", "Could not get keepalived process: "+err.Error())
	}

	if !keepalivedRunning {
		return UpdateLBMConditions(ctx, c, lbm,
			KeepalivedProcess,
			ConditionFalse,
			"CouldNotKeepalivedProcess", "Keepalived is not running")
	}

	return UpdateLBMConditions(ctx, c, lbm,
		KeepalivedProcess,
		ConditionTrue,
		"KeepalivedIsRunning", "Keepalived is running with PID: "+string(pidID))
}

// EnableAdHocDebugging enables ad-hoc debugging if enabled via annotations.
func EnableAdHocDebugging(
	lb *yawolv1beta1.LoadBalancer,
	lbm *yawolv1beta1.LoadBalancerMachine,
	recorder record.EventRecorder,
	lbmName string,
) error {
	enabled, _ := strconv.ParseBool(lb.Annotations[yawolv1beta1.LoadBalancerAdHocDebug])
	sshKey, sshKeySet := lb.Annotations[yawolv1beta1.LoadBalancerAdHocDebugSSHKey]

	// skip not all needed annotations are set or disabled
	if !enabled || !sshKeySet {
		return nil
	}

	// skip if debugging is enabled anyway
	if lb.Spec.DebugSettings.Enabled {
		recorder.Event(lbm, corev1.EventTypeWarning,
			"AdHocDebuggingNotEnabled",
			"Ad-hoc debugging will not be enabled because normal debug with is already enabled")
		return nil
	}

	if !rxSSHPublicKey.MatchString(sshKey) {
		recorder.Event(lbm, corev1.EventTypeWarning,
			"AdHocDebuggingNotEnabled",
			"Ad-hoc debugging will not be enabled because ssh key is not valid")
		return nil
	}

	addAuthorizedKeys := exec.Command( //nolint: gosec // sshKey can only be a ssh public key checked by regex
		"/bin/sh",
		"-c",
		"echo \"\n"+sshKey+"\n\" | sudo tee /home/yawoldebug/.ssh/authorized_keys",
	)
	if err := addAuthorizedKeys.Run(); err != nil {
		recorder.Eventf(lbm, corev1.EventTypeWarning,
			"AdHocDebuggingNotEnabled",
			"Ad-hoc debugging cant be enabled because authorized_keys cant be written: %v", err)
		return nil // no error to be sure the loadbalancer still will be reconciled
	}

	startSSH := exec.Command("sudo", "/sbin/rc-service", "sshd", "start")
	if err := startSSH.Run(); err != nil {
		recorder.Eventf(lbm, corev1.EventTypeWarning,
			"AdHocDebuggingNotEnabled",
			"Ad-hoc debugging cant be enabled because ssh cant be started: %v", err)
		return nil // no error to be sure the loadbalancer still will be reconciled
	}

	recorder.Eventf(lbm, corev1.EventTypeWarning,
		"AdHocDebuggingEnabled",
		"Successfully enabled ad-hoc debugging access to LoadBalancerMachine '%s'. "+
			"Please make sure to disable debug access once you are finished and to roll all LoadBalancerMachines", lbmName)
	return nil
}
