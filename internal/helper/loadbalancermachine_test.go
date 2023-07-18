package helper

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
