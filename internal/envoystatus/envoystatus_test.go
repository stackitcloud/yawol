package envoystatus

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/yawol/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var wanted = []v1beta1.LoadBalancerMachineMetric{
	{
		Type:  "TCP-80-upstream_cx_active",
		Value: "1",
		Time:  metav1.Time{},
	},
	{
		Type:  "TCP-80-upstream_cx_total",
		Value: "16",
		Time:  metav1.Time{},
	},
	{
		Type:  "TCP-80-upstream_cx_connect_fail",
		Value: "5",
		Time:  metav1.Time{},
	},
	{
		Type:  "TCP-80-upstream_cx_rx_bytes_total",
		Value: "11405",
		Time:  metav1.Time{},
	},
	{
		Type:  "TCP-80-upstream_cx_tx_bytes_total",
		Value: "2984",
		Time:  metav1.Time{},
	},
	{
		Type:  "envoytcp-idle_timeout",
		Value: "2",
		Time:  metav1.Time{},
	},
}

var envoyMetrics = `
cluster.TCP-80.upstream_cx_active: 1
cluster.TCP-80.upstream_cx_total: 16
cluster.TCP-80.upstream_cx_connect_fail: 5
cluster.TCP-80.upstream_cx_tx_bytes_total: 2984
cluster.TCP-80.upstream_cx_rx_bytes_total: 11405
tcp.envoytcp.idle_timeout: 2
`

func TestEnvoyStats4(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "")
}

var _ = Describe("check envoy status", func() {
	When("Envoy stats to LoadBalancerMachine Metrics", func() {
		It("should set the correct metrics", func() {
			got := parseEnvoyMetricsToLoadBalancerMachineMetrics(envoyMetrics)
			Expect(compareMetrics(got, wanted)).To(BeTrue())

		})
	})
})

func compareMetrics(got, wanted []v1beta1.LoadBalancerMachineMetric) bool {
	for iWant := range wanted {
		found := false
		for iGot := range got {
			if wanted[iWant].Type != got[iGot].Type {
				continue
			}
			found = true
			if wanted[iWant].Value != got[iGot].Value {
				return false
			}
			break
		}
		if !found {
			return false
		}
	}
	return true
}
