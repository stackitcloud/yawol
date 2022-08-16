package openstack

import "github.com/prometheus/client_golang/prometheus"

// increasePromCounter increase a prometheus.CounterVec with a specific label.
// Ignored if promCounter is nil.
func increasePromCounter(promCounter *prometheus.CounterVec, label string) {
	if promCounter == nil {
		return
	}
	promCounter.WithLabelValues(label).Inc()
}
