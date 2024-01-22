package helper

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
)

var _ = Describe("loadbalancermachine", Serial, Ordered, func() {
	Describe("promtail config", func() {
		When("external labels are set", func() {
			It("should render them in the promtail config", func() {
				logForward := yawolv1beta1.LoadBalancerLogForward{
					LokiURL: "localhost",
					Labels: map[string]string{
						"foo": "bar",
						"env": "testing",
					},
				}

				config, err := generatePromtailConfig("some-lb", "some-lbm", logForward)
				Expect(err).ToNot(HaveOccurred())

				expected := `server:
  disable: true

positions:
  filename: /tmp/positions.yaml

clients:
  - url: 'localhost'

scrape_configs:
  - job_name: messages
    static_configs:
      - targets:
          - localhost
        labels:
          __path__: /var/log/messages
          application: messages
          env: testing
          foo: bar
          job: yawol-logs
          lb: some-lb
          lbm: some-lbm
          `
				Expect(config).To(Equal(expected))
			})
		})
	})
})

type relCondTest struct {
	conditions      *[]corev1.NodeCondition
	expiration      metav1.Time
	checkTransition bool
	expect          bool
	expectReason    string
}

var _ = DescribeTable("areRelevantConditionsMet",
	func(t relCondTest) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: t.conditions,
			},
		}
		res, reason := AreRelevantConditionsMet(machine, t.expiration, t.checkTransition)
		if t.expectReason != "" {
			Expect(reason).To(ContainSubstring(t.expectReason))
		}
		Expect(res).To(Equal(t.expect))
	},
	Entry("No Conditions", relCondTest{
		conditions: nil,
		expect:     false,
	}),
	Entry("empty Conditions", relCondTest{
		conditions: &[]corev1.NodeCondition{},
		expect:     false,
	}),
	Entry("all conditions met", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(ConfigReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: false,
		expect:          true,
	}),
	Entry("a required condition is missing", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		expect:       false,
		expectReason: "required condition ConfigReady not present on machine",
	}),
	Entry("a unrelated condition is fale", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(ConfigReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
			{Type: "foo", Status: corev1.ConditionFalse},
		},
		expect: true,
	}),
	Entry("a required condition is false", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(ConfigReady), Status: corev1.ConditionFalse},
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		expect:       false,
		expectReason: "condition ConfigReady is in status False",
	}),
	Entry("a required condition is too old", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{
				Type:              corev1.NodeConditionType(ConfigReady),
				Status:            corev1.ConditionTrue,
				LastHeartbeatTime: metav1.Time{Time: time.Time{}.Add(-5 * time.Minute)},
			},
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: false,
		expect:          false,
		expectReason:    "condition ConfigReady heartbeat is stale",
	}),
	Entry("with transition check: a condition is failed, but just happened", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{
				Type:               corev1.NodeConditionType(ConfigReady),
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Time{},
			},
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: true,
		expect:          true,
	}),
	Entry("with transition check: a condition is failed, some time ago", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{
				Type:               corev1.NodeConditionType(ConfigReady),
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Time{Time: time.Time{}.Add(-5 * time.Minute)},
			},
			{Type: corev1.NodeConditionType(EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: true,
		expect:          false,
	}),
)
