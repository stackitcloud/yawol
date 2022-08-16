package controllers

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	"dev.azure.com/schwarzit/schwarzit.ske/yawol.git/internal/helper"
)

const StatusConditions int = 3

var _ = Describe("Check loadbalancer reconcile", func() {
	Context("run tests", func() {
		ctx := context.Background()
		var lb yawolv1beta1.LoadBalancer
		var lbm yawolv1beta1.LoadBalancerMachine

		It("Create initial loadbalancer and loadbalancermachine", func() {
			By("create testns namespace")
			ns := v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "testns"}}
			Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

			By("create test-lbm loadbalancermachine")
			lbm = yawolv1beta1.LoadBalancerMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lbm",
					Namespace: "testns"},
				Spec: yawolv1beta1.LoadBalancerMachineSpec{
					FloatingID:      "",
					Infrastructure:  yawolv1beta1.LoadBalancerInfrastructure{},
					LoadBalancerRef: yawolv1beta1.LoadBalancerRef{},
				},
			}
			Expect(k8sClient.Create(ctx, &lbm)).Should(Succeed())

			By("create test-lb loadbalancer")
			lb = yawolv1beta1.LoadBalancer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lb",
					Namespace: "testns"},
				Spec: yawolv1beta1.LoadBalancerSpec{
					Selector: metav1.LabelSelector{},
					Replicas: 1,
					Options: yawolv1beta1.LoadBalancerOptions{
						InternalLB:               false,
						LoadBalancerSourceRanges: nil,
					},
					Endpoints: []yawolv1beta1.LoadBalancerEndpoint{{
						Name:      "localhost",
						Addresses: []string{"127.0.0.1"},
					}},
					Ports: []v1.ServicePort{{
						Name:       "port",
						Protocol:   "TCP",
						Port:       8081,
						TargetPort: intstr.IntOrString{IntVal: 8081},
						NodePort:   12456,
					}},
					Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{},
				},
			}
			Expect(k8sClient.Create(ctx, &lb)).Should(Succeed())
		})
		It("test initial snapshot creation with condition", func() {
			By("check conditions")
			Eventually(func() error {
				return checkConditions(
					ctx,
					"test-lbm",
					"testns",
					helper.ConditionTrue,
					helper.ConditionTrue,
					helper.ConditionTrue,
					"TCP-8081::127.0.0.1:8081",
				)
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add port to loadbalancer check if new port is set", func() {
			By("add new port")
			// add second port
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port2",
				Protocol:   "TCP",
				Port:       8082,
				TargetPort: intstr.IntOrString{IntVal: 8082},
				NodePort:   12457,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if new port is present")
			Eventually(func() error {
				return checkConditions(
					ctx,
					"test-lbm",
					"testns",
					helper.ConditionTrue,
					helper.ConditionTrue,
					helper.ConditionTrue,
					"TCP-8082::127.0.0.1:8082",
				)
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add source ranges and check if ports are ready", func() {
			By("add source ranges")
			lb.Spec.Options.LoadBalancerSourceRanges = []string{
				"127.0.0.1/24",
				"2002::1234:abcd:ffff:c0a8:101/64",
			}
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if ports are present")
			Eventually(func() error {
				return checkConditions(
					ctx,
					"test-lbm",
					"testns",
					helper.ConditionTrue,
					helper.ConditionTrue,
					helper.ConditionTrue,
					"TCP-8081::127.0.0.1:8081",
				)
			}, time.Second*15, time.Second*1).Should(Succeed())

			Eventually(func() error {
				return checkConditions(
					ctx,
					"test-lbm",
					"testns",
					helper.ConditionTrue,
					helper.ConditionTrue,
					helper.ConditionTrue,
					"TCP-8082::127.0.0.1:8082",
				)
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("remove source ranges")
			lb.Spec.Options.LoadBalancerSourceRanges = nil
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())
		})
		It("set envoy to fail and check envoyReady conditions", func() {
			By("set envoy ready status to fail")
			_, err := http.Post("http://127.0.0.1:9000/healthcheck/fail", "", nil)
			Expect(err).ToNot(HaveOccurred())

			By("check if envoyReady condition is False")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, helper.ConditionFalse, helper.ConditionTrue, "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("set envoy ready back to ok")
			_, err = http.Post("http://127.0.0.1:9000/healthcheck/ok", "", nil)
			Expect(err).Should(Succeed())

			By("check if envoyReady condition is True")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", "", helper.ConditionTrue, "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add port with bad protocol", func() {
			By("add new port")
			// add second port
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "BLA",
				Port:       8083,
				TargetPort: intstr.IntOrString{IntVal: 8083},
				NodePort:   12458,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionFalse, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete bad protocol port")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add port with udp protocol", func() {
			By("add new port")
			// add second port
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "UDP",
				Port:       8083,
				TargetPort: intstr.IntOrString{IntVal: 8083},
				NodePort:   12458,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete correct protocol port")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add port with correct protocol", func() {
			By("add new port")
			// add second port
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       8083,
				TargetPort: intstr.IntOrString{IntVal: 8083},
				NodePort:   12458,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete correct protocol port")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add new Port with Port out of range ", func() {
			By("add too high port")
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       65536,
				TargetPort: intstr.IntOrString{IntVal: 65536},
				NodePort:   12458,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionFalse, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete too high port")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("add too low port")
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       0, // if port is not set, port will automatically be 0
				TargetPort: intstr.IntOrString{IntVal: 0},
				NodePort:   12444,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionFalse, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete too low port")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add new Port with Port in valid range", func() {
			By("add valid new port")
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       32666,
				TargetPort: intstr.IntOrString{IntVal: 65536},
				NodePort:   12458,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete correct port")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add new Port with NodePort out of range", func() {
			By("add too high NodePort")
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       8083,
				TargetPort: intstr.IntOrString{IntVal: 0},
				NodePort:   65536,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionFalse, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete too high NodePort")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("add too low NodePort")
			oldPorts = lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       8083,
				TargetPort: intstr.IntOrString{IntVal: 0},
				NodePort:   0,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("Check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete too low NodePort")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add new Port with NodePort in valid range", func() {
			By("add valid new NodePort")
			oldPorts := lb.Spec.Ports
			lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
				Name:       "port3",
				Protocol:   "TCP",
				Port:       8083,
				TargetPort: intstr.IntOrString{IntVal: 0},
				NodePort:   32356,
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete valid NodePort")
			lb.Spec.Ports = oldPorts
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add Endpoint with wrong IPv4 Address", func() {
			By("add wrong IPv4 Address in Endpoint")
			oldEndpoints := lb.Spec.Endpoints
			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"124.34.3"},
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete Endpoint with wrong IPv4 Address")
			lb.Spec.Endpoints = oldEndpoints
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add Endpoint with valid IPv4 Address", func() {
			By("add correct IPv4 Address in Endpoint")
			oldEndpoints := lb.Spec.Endpoints
			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"124.34.3.5"},
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete Endpoint with correct IP Address")
			lb.Spec.Endpoints = oldEndpoints
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add Endpoint with wrong IPv6 Address", func() {
			By("add wrong IPv6 Address in Endpoint")
			oldEndpoints := lb.Spec.Endpoints
			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"2001:0db8:53a1:7734"},
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionFalse, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete Endpoint with wrong IPv6 Address")
			lb.Spec.Endpoints = oldEndpoints
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add Endpoint with valid IPv6 Address", func() {
			By("add valid IPv6 Address in Endpoint")
			oldEndpoints := lb.Spec.Endpoints
			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete Endpoint with valid IPv6 Address")
			lb.Spec.Endpoints = oldEndpoints
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add Endpoint with wrong domain name", func() {
			By("add wrong domain name in Endpoint")
			oldEndpoints := lb.Spec.Endpoints
			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"abc..de"},
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config fails")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete Endpoint with wrong DNS name")
			lb.Spec.Endpoints = oldEndpoints
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("add Endpoint with valid domain name", func() {
			By("add valid domain name in Endpoint")
			oldEndpoints := lb.Spec.Endpoints
			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"abc.de"},
			})
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("delete Endpoint with correct DNS name")
			lb.Spec.Endpoints = oldEndpoints
			Expect(k8sClient.Update(ctx, &lb)).Should(Succeed())

			By("check if config is successful")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", helper.ConditionTrue, "", "", "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
		It("test envoy not up to date", func() {
			By("kill envoy process")
			err := envoyCmd.Process.Kill()
			Expect(err).ToNot(HaveOccurred())

			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint3",
				Addresses: []string{"127.0.0.1"},
			})

			By("check if EnvoyUpToDate is False")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", "", "", helper.ConditionFalse, "")
			}, time.Second*15, time.Second*1).Should(Succeed())

			By("start envoy process")
			envoyCmd = exec.Command("envoy", "-c", "../../image/envoy-config.yaml")
			err = envoyCmd.Start()
			Expect(err).ToNot(HaveOccurred())

			lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
				Name:      "Endpoint4",
				Addresses: []string{"127.42.0.69"},
			})

			By("check if EnvoyUpToDate is True")
			Eventually(func() error {
				return checkConditions(ctx, "test-lbm", "testns", "", "", helper.ConditionTrue, "")
			}, time.Second*15, time.Second*1).Should(Succeed())
		})
	})
})

func checkConditions(
	ctx context.Context,
	name, namespace string,
	configReady, envoyReady, envoyUpToDate helper.LoadbalancerConditionStatus,
	listener string,
) error {
	var curLbm yawolv1beta1.LoadBalancerMachine
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &curLbm)
	if err != nil {
		return err
	}
	if curLbm.Status.Conditions == nil || len(*curLbm.Status.Conditions) < StatusConditions {
		return helper.ErrNotAllConditionsSet
	}
	for _, condition := range *curLbm.Status.Conditions {
		switch string(condition.Type) {
		case string(helper.ConfigReady):
			if configReady != "" && string(condition.Status) != string(configReady) {
				return helper.ErrConfigNotReady
			}
		case string(helper.EnvoyReady):
			if envoyReady != "" && string(condition.Status) != string(envoyReady) {
				return helper.ErrEnvoyNotReady
			}
		case string(helper.EnvoyUpToDate):
			if envoyUpToDate != "" && string(condition.Status) != string(envoyUpToDate) {
				return helper.ErrEnvoyNotUpToDate
			}
		}
	}
	if listener != "" {
		var resp *http.Response
		resp, err = http.Get("http://127.0.0.1:9000/listeners")
		if err != nil {
			return err
		}
		defer resp.Body.Close() //nolint:errcheck // don't handle error in defer
		// check if not 200 and return an empty snapshot version if status code is not 200
		if resp.StatusCode != 200 {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		// output should contain:
		// TCP-8081::127.0.0.1:8081
		for _, stats := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(stats, listener) {
				return nil
			}
		}
		return helper.ErrEnvoyListenerNotFound
	}
	return nil
}
