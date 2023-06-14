package controllers

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
)

const StatusConditions int = 3
const (
	TIMEOUT  = 10 * time.Second
	INTERVAL = 500 * time.Millisecond
)

var _ = Describe("check loadbalancer reconcile", Serial, Ordered, func() {

	It("should create the namespace", func() {
		ns := v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace}}
		Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())
		Eventually(func() {
			Expect(k8sClient.Get(ctx, kclient.ObjectKeyFromObject(&ns), &ns)).Should(Succeed())
		})
	})

	Context("namespace is created", func() {
		ctx := context.Background()
		var lb yawolv1beta1.LoadBalancer
		var lbm yawolv1beta1.LoadBalancerMachine

		BeforeEach(func() {
			lbm = yawolv1beta1.LoadBalancerMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameLBM,
					Namespace: Namespace,
					Annotations: map[string]string{
						helper.RevisionAnnotation: "1",
					},
				},
				Spec: yawolv1beta1.LoadBalancerMachineSpec{
					PortID:          "",
					Infrastructure:  yawolv1beta1.LoadBalancerInfrastructure{},
					LoadBalancerRef: yawolv1beta1.LoadBalancerRef{},
				},
			}

			lb = yawolv1beta1.LoadBalancer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameLB,
					Namespace: Namespace,
					Annotations: map[string]string{
						helper.RevisionAnnotation: "1",
					},
				},
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
		})

		JustBeforeEach(func() {
			Expect(k8sClient.Create(ctx, &lbm)).Should(Succeed())
			Expect(k8sClient.Create(ctx, &lb)).Should(Succeed())

			Eventually(func() {
				Expect(k8sClient.Get(
					ctx, kclient.ObjectKeyFromObject(&lb), &yawolv1beta1.LoadBalancer{},
				)).To(Succeed())

				Expect(k8sClient.Get(
					ctx, kclient.ObjectKeyFromObject(&lbm), &yawolv1beta1.LoadBalancerMachine{},
				)).To(Succeed())
			})
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &lbm)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &lb)).Should(Succeed())

			Eventually(func() {
				var err error
				err = k8sClient.Get(
					ctx, kclient.ObjectKeyFromObject(&lb), &yawolv1beta1.LoadBalancer{},
				)
				Expect(kclient.IgnoreNotFound(err)).To(Succeed())
				// we expect a not found error
				Expect(err).To(Not(BeNil()))

				err = k8sClient.Get(
					ctx, kclient.ObjectKeyFromObject(&lbm), &yawolv1beta1.LoadBalancerMachine{},
				)
				Expect(kclient.IgnoreNotFound(err)).To(Succeed())
				// we expect a not found error
				Expect(err).To(Not(BeNil()))
			})
		})

		It("should create the initial snapshot", func() {
			eventuallyCheckConditions(
				ctx,
				helper.ConditionTrue,
				helper.ConditionTrue,
				helper.ConditionTrue,
				"TCP-8081::127.0.0.1:8081",
			)
		})

		When("adding a new port", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port2",
					Protocol:   "TCP",
					Port:       8082,
					TargetPort: intstr.IntOrString{IntVal: 8082},
					NodePort:   12457,
				})
			})

			It("should create the corresponding listener", func() {
				eventuallyCheckConditions(
					ctx,
					helper.ConditionTrue,
					helper.ConditionTrue,
					helper.ConditionTrue,
					"TCP-8082::127.0.0.1:8082",
				)
			})
		})

		When("adding source ranges", func() {
			BeforeEach(func() {
				lb.Spec.Options.LoadBalancerSourceRanges = []string{
					"127.0.0.1/24",
					"2002::1234:abcd:ffff:c0a8:101/64",
				}
			})

			It("should create the corresponding listener", func() {
				eventuallyCheckConditions(
					ctx,
					helper.ConditionTrue,
					helper.ConditionTrue,
					helper.ConditionTrue,
					"TCP-8081::127.0.0.1:8081",
				)
			})
		})

		When("setting envoy healthcheck to fail", func() {
			BeforeEach(func() {
				_, err := http.Post("http://127.0.0.1:9000/healthcheck/fail", "", nil)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				_, err := http.Post("http://127.0.0.1:9000/healthcheck/ok", "", nil)
				Expect(err).Should(Succeed())
			})

			It("should have false ready condition", func() {
				eventuallyCheckConditions(
					ctx,
					helper.ConditionTrue,
					helper.ConditionFalse,
					helper.ConditionTrue,
					"TCP-8081::127.0.0.1:8081",
				)
			})
		})

		When("setting a bad protocol in the port", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "BLA",
					Port:       8083,
					TargetPort: intstr.IntOrString{IntVal: 8083},
					NodePort:   12458,
				})
			})

			It("should have false condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionFalse, "", "", "",
				)
			})
		})

		When("adding a port with udp protocol", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "UDP",
					Port:       8083,
					TargetPort: intstr.IntOrString{IntVal: 8083},
					NodePort:   12458,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})

		When("adding a port with tcp protocol", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       8083,
					TargetPort: intstr.IntOrString{IntVal: 8083},
					NodePort:   12458,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})

		When("adding a port that is out of range (too high)", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       65536,
					TargetPort: intstr.IntOrString{IntVal: 65536},
					NodePort:   12458,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding a port that is out of range (too low)", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       0,
					TargetPort: intstr.IntOrString{IntVal: 0},
					NodePort:   12458,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding a port in the valid range", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       32666,
					TargetPort: intstr.IntOrString{IntVal: 65536},
					NodePort:   12458,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})

		When("adding a nodeport that is out of range (too high)", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       8083,
					TargetPort: intstr.IntOrString{IntVal: 0},
					NodePort:   65536,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding a nodeport that is out of range (too low)", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       8083,
					TargetPort: intstr.IntOrString{IntVal: 0},
					NodePort:   0,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding a nodeport that is in valid range", func() {
			BeforeEach(func() {
				lb.Spec.Ports = append(lb.Spec.Ports, v1.ServicePort{
					Name:       "port3",
					Protocol:   "TCP",
					Port:       8083,
					TargetPort: intstr.IntOrString{IntVal: 0},
					NodePort:   32356,
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})

		When("adding an endpoint with wrong ipv4 address", func() {
			BeforeEach(func() {
				lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
					Name:      "Endpoint3",
					Addresses: []string{"124.34.3"},
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding a port with a valid ipv4 address", func() {
			BeforeEach(func() {
				lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
					Name:      "Endpoint3",
					Addresses: []string{"124.34.3.5"},
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})

		When("adding an endpoint with a wrong ipv6 address", func() {
			BeforeEach(func() {
				lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
					Name:      "Endpoint3",
					Addresses: []string{"2001:0db8:53a1:7734"},
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding an endpoint with a valid ipv6 address", func() {
			BeforeEach(func() {
				lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
					Name:      "Endpoint3",
					Addresses: []string{"2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})

		When("adding an endpoint with a wrong domain name", func() {
			BeforeEach(func() {
				lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
					Name:      "Endpoint3",
					Addresses: []string{"abc..de"},
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(ctx, helper.ConditionFalse, "", "", "")
			})
		})

		When("adding an endpoint with a valid domain name", func() {
			BeforeEach(func() {
				lb.Spec.Endpoints = append(lb.Spec.Endpoints, yawolv1beta1.LoadBalancerEndpoint{
					Name:      "Endpoint3",
					Addresses: []string{"abc.de"},
				})
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, "", "", "",
				)
			})
		})

		When("enabling proxy protocol", func() {
			BeforeEach(func() {
				lb.Spec.Options.TCPProxyProtocol = true
			})

			It("should have the correct condition", func() {
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)
			})
		})
		When("lb and lbm revision annotation are the same", func() {
			It("should create yawolKeepalivedFile", func() {
				Eventually(func() error {
					_, err := filesystem.Stat(helper.YawolSetIsLatestRevisionFile)
					return err
				}, TIMEOUT, INTERVAL).Should(Succeed())

			})
		})
		When("lb and lbm revision annotation are the same", func() {
			BeforeEach(func() {
				lb.Annotations = map[string]string{
					helper.RevisionAnnotation: "2",
				}
			})
			It("should not create or delete yawolKeepalivedFile", func() {
				Eventually(func() error {
					_, err := filesystem.Stat(helper.YawolSetIsLatestRevisionFile)
					if err == nil || !errors.Is(err, fs.ErrNotExist) {
						return errors.New("keepalived file still exists")
					}
					return nil
				}, TIMEOUT, INTERVAL).Should(Succeed())

			})
		})
		When("envoy gets killed and restarted", func() {
			It("should set the correct conditions", func() {
				By("killing the envoy process")
				err := envoyCmd.Process.Kill()
				Expect(err).ToNot(HaveOccurred())

				By("checking if EnvoyUpToDate is False")
				eventuallyCheckConditions(ctx, "", "", helper.ConditionFalse, "")

				By("get transition time")
				lbmBeforeTransition, err := getCurrentLBM(ctx)
				Expect(err).To(BeNil())
				Expect(lbmBeforeTransition.Status.Conditions).NotTo(BeNil())
				var lbmBeforeTransitionTime metav1.Time
				for _, condition := range *lbmBeforeTransition.Status.Conditions {
					if string(condition.Type) == string(helper.EnvoyReady) {
						lbmBeforeTransitionTime = condition.LastTransitionTime
					}
				}

				By("starting the envoy process")
				envoyCmd = exec.Command("envoy", "-c", "../../image/envoy-config.yaml")
				Expect(envoyCmd.Start()).To(Succeed())

				By("checking if EnvoyUpToDate is True")
				eventuallyCheckConditions(
					ctx, helper.ConditionTrue, helper.ConditionTrue, helper.ConditionTrue, "",
				)

				By("get transition time")
				lbmAfterTransition, err := getCurrentLBM(ctx)
				Expect(err).To(BeNil())
				Expect(lbmAfterTransition.Status.Conditions).NotTo(BeNil())
				var lbmAfterTransitionTime metav1.Time
				for _, condition := range *lbmAfterTransition.Status.Conditions {
					if string(condition.Type) == string(helper.EnvoyReady) {
						lbmAfterTransitionTime = condition.LastTransitionTime
					}
				}

				By("compare transition time")
				Expect(lbmAfterTransitionTime.After(lbmBeforeTransitionTime.Time)).To(BeTrue())

			})
		})

	})
})

func eventuallyCheckConditions(
	ctx context.Context,
	configReady, envoyReady, envoyUpToDate helper.LoadbalancerConditionStatus,
	listener string,
) {
	Eventually(func() error {
		return checkConditions(
			ctx, configReady, envoyReady, envoyUpToDate, listener,
		)
	}, TIMEOUT, INTERVAL).Should(Succeed())
}

func checkCondition(
	conditionType helper.LoadbalancerCondition,
	condition helper.LoadbalancerConditionStatus,
	conditions []v1.NodeCondition,
) bool {
	for _, c := range conditions {
		if string(c.Type) != string(conditionType) {
			continue
		}

		return string(c.Status) == string(condition)
	}
	return false
}

func checkConditions(
	ctx context.Context,
	configReady, envoyReady, envoyUpToDate helper.LoadbalancerConditionStatus,
	listener string,
) error {
	curLbm, err := getCurrentLBM(ctx)
	if err != nil {
		return err
	}

	if curLbm.Status.Conditions == nil {
		return helper.ErrNotAllConditionsSet
	}

	if configReady != "" {
		if !checkCondition(helper.ConfigReady, configReady, *curLbm.Status.Conditions) {
			return helper.ErrConfigNotReady
		}
	}

	if envoyReady != "" {
		if !checkCondition(helper.EnvoyReady, envoyReady, *curLbm.Status.Conditions) {
			return helper.ErrEnvoyNotReady
		}
	}

	if envoyUpToDate != "" {
		if !checkCondition(helper.EnvoyUpToDate, envoyUpToDate, *curLbm.Status.Conditions) {
			return helper.ErrEnvoyNotUpToDate
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

func getCurrentLBM(ctx context.Context) (yawolv1beta1.LoadBalancerMachine, error) {
	var curLbm yawolv1beta1.LoadBalancerMachine
	err := k8sClient.Get(ctx, types.NamespacedName{Name: nameLBM, Namespace: Namespace}, &curLbm)
	if err != nil {
		return yawolv1beta1.LoadBalancerMachine{}, err
	}
	return curLbm, nil
}
