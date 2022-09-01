package openstack

import "github.com/prometheus/client_golang/prometheus"

type MetricAPI string
type MetricObject string
type MetricOperation string

const (
	MetricAPINova    MetricAPI = "nova"
	MetricAPINeutron MetricAPI = "neutron"
)

const (
	MetricObjectFloatingIP MetricObject = "floatingip"
	MetricObjectGroup      MetricObject = "group"
	MetricObjectKeyPair    MetricObject = "keypair"
	MetricObjectPort       MetricObject = "port"
	MetricObjectRule       MetricObject = "rule"
	MetricObjectServer     MetricObject = "server"
)

const (
	MetricOperationCreate MetricOperation = "create"
	MetricOperationDelete MetricOperation = "delete"
	MetricOperationGet    MetricOperation = "get"
	MetricOperationList   MetricOperation = "list"
	MetricOperationUpdate MetricOperation = "update"
)

// increasePromCounter increase a prometheus.CounterVec with a specific label.
// Ignored if promCounter is nil.
func increasePromCounter(promCounter *prometheus.CounterVec, api MetricAPI, object MetricObject, operation MetricOperation) {
	if promCounter == nil {
		return
	}
	promCounter.WithLabelValues(string(api), string(object), string(operation)).Inc()
}
