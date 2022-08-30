package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// overall
	OpenstackMetrics = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "openstack",
		Help: "Metrics of Openstack API usage",
	}, []string{"API"})

	// loadbalancer controller
	LoadBalancerInfoMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancer_info",
		Help: "Info Metric for LoadBalancer",
	}, []string{"lb", "namespace", "isInternal", "tcpProxyProtocol", "externalIP"})
	LoadBalancerOpenstackMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancer_openstack_info",
		Help: "Openstack Info Metric for LoadBalancer",
	}, []string{"lb", "namespace", "portID", "floatingID", "securityGroupID"})
	LoadBalancerReplicasMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancer_replicas",
		Help: "Replicas for LoadBalancer",
	}, []string{"lb", "namespace"})
	LoadBalancerReplicasCurrentMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancer_replicas_current",
		Help: "Current replicas for LoadBalancer",
	}, []string{"lb", "namespace"})
	LoadBalancerReplicasReadyMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancer_replicas_ready",
		Help: "Ready replicas for LoadBalancer",
	}, []string{"lb", "namespace"})

	// loadbalancerset controller
	LoadBalancerSetReplicasMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancerset_replicas",
		Help: "Replicas for LoadBalancerSet",
	}, []string{"lb", "lbs", "namespace"})
	LoadBalancerSetReplicasCurrentMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancerset_replicas_current",
		Help: "Current replicas for LoadBalancerSet",
	}, []string{"lb", "lbs", "namespace"})
	LoadBalancerSetReplicasReadyMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancerset_replicas_ready",
		Help: "Ready replicas for LoadBalancerSet",
	}, []string{"lb", "lbs", "namespace"})

	// loadbalancermachine controller
	LoadBalancerMachineMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadbalancermachine",
		Help: "Metrics of machine",
	}, []string{"type", "lb", "lbm", "namespace"})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(OpenstackMetrics)
	metrics.Registry.MustRegister(LoadBalancerInfoMetrics)
	metrics.Registry.MustRegister(LoadBalancerOpenstackMetrics)
	metrics.Registry.MustRegister(LoadBalancerReplicasMetrics)
	metrics.Registry.MustRegister(LoadBalancerReplicasCurrentMetrics)
	metrics.Registry.MustRegister(LoadBalancerReplicasReadyMetrics)
	metrics.Registry.MustRegister(LoadBalancerSetReplicasMetrics)
	metrics.Registry.MustRegister(LoadBalancerSetReplicasCurrentMetrics)
	metrics.Registry.MustRegister(LoadBalancerSetReplicasReadyMetrics)
	metrics.Registry.MustRegister(LoadBalancerMachineMetrics)
}
