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
    proxy_url: ''

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
		When("proxy url is set", func() {
			It("should render it in the promtail config", func() {
				logForward := yawolv1beta1.LoadBalancerLogForward{
					LokiURL:  "localhost",
					ProxyURL: "proxy-url",
				}

				config, err := generatePromtailConfig("some-lb", "some-lbm", logForward)
				Expect(err).ToNot(HaveOccurred())

				expected := `server:
  disable: true

positions:
  filename: /tmp/positions.yaml

clients:
  - url: 'localhost'
    proxy_url: 'proxy-url'

scrape_configs:
  - job_name: messages
    static_configs:
      - targets:
          - localhost
        labels:
          __path__: /var/log/messages
          application: messages
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

var _ = Describe("generateChronyConfig", func() {
	var (
		pools, servers []string
	)

	BeforeEach(func() {
		pools = []string{"pool.a.org", "pool.b.org"}
		servers = []string{"ntp.a.org", "10.0.10.1"}
	})

	It("should use pool.ntp.org if neither pools nor servers are specified", func() {
		Expect(generateChronyConfig(nil, nil)).To(BeEquivalentTo(`pool pool.ntp.org iburst
# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
`))
	})

	It("should use the configured pools", func() {
		Expect(generateChronyConfig(pools[:1], nil)).To(BeEquivalentTo(`pool pool.a.org iburst
# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
`))

		Expect(generateChronyConfig(pools, nil)).To(BeEquivalentTo(`pool pool.a.org iburst
pool pool.b.org iburst
# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
`))
	})

	It("should use the configured servers", func() {
		Expect(generateChronyConfig(nil, servers[:1])).To(BeEquivalentTo(`server ntp.a.org iburst
# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
`))

		Expect(generateChronyConfig(nil, servers)).To(BeEquivalentTo(`server ntp.a.org iburst
server 10.0.10.1 iburst
# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
`))
	})

	It("should use both the configured pools and servers", func() {
		Expect(generateChronyConfig(pools, servers)).To(BeEquivalentTo(`pool pool.a.org iburst
pool pool.b.org iburst
server ntp.a.org iburst
server 10.0.10.1 iburst
# Settings from alpine default chrony config
driftfile /var/lib/chrony/chrony.drift
rtcsync
# prevent chrony from opening ports on the LoadBalancer machine
cmdport 0

# Settings from cloud-init generated chrony config
# Stop bad estimates upsetting machine clock.
maxupdateskew 100.0
# Step the system clock instead of slewing it if the adjustment is larger than
# one second, but only in the first three clock updates.
makestep 1 3
`))
	})
})
