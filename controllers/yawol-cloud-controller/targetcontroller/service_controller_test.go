package targetcontroller

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check loadbalancer reconcile", Serial, Ordered, func() {
	Context("run tests", func() {
		ctx := context.Background()
		var lb yawolv1beta1.LoadBalancer

		It("create service and check port", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test1",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30001,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check ports in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Ports == nil || len(lb.Spec.Ports) != 1 {
					return helper.ErrZeroOrMoreThanOnePortFoundInLB
				}
				if lb.Spec.Ports[0].Name == "port1" &&
					lb.Spec.Ports[0].Port == 12345 &&
					lb.Spec.Ports[0].Protocol == v1.ProtocolTCP &&
					lb.Spec.Ports[0].NodePort == 30001 {
					return nil
				}
				return helper.ErrPortValuesWrong
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for creation")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test1" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer is in creation") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for port sync")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test1" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer ports successfully synced with service ports") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service for changing protocol", func() {
			By("create UDP service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test-changing-port",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolUDP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30020,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check udp protocol in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test-changing-port", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Ports == nil || len(lb.Spec.Ports) != 1 {
					return helper.ErrZeroOrMoreThanOnePortFoundInLB
				}
				if lb.Spec.Ports[0].Name == "port1" &&
					lb.Spec.Ports[0].Port == 65000 &&
					lb.Spec.Ports[0].Protocol == v1.ProtocolUDP &&
					lb.Spec.Ports[0].NodePort == 30020 {
					return nil
				}
				return helper.ErrPortValuesWrong
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("change protocol to tcp")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.Spec.Ports = []v1.ServicePort{
				{
					Name:       "tcp-edit",
					Protocol:   v1.ProtocolTCP,
					Port:       65000,
					TargetPort: intstr.IntOrString{IntVal: 12345},
					NodePort:   30020,
				},
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check changed protocol in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test-changing-port", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Ports == nil || len(lb.Spec.Ports) != 1 {
					return helper.ErrZeroOrMoreThanOnePortFoundInLB
				}
				if lb.Spec.Ports[0].Name == "tcp-edit" &&
					lb.Spec.Ports[0].Port == 65000 &&
					lb.Spec.Ports[0].Protocol == v1.ProtocolTCP &&
					lb.Spec.Ports[0].NodePort == 30020 {
					return nil
				}
				return helper.ErrPortValuesWrong
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("change protocol to udp")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.Spec.Ports = []v1.ServicePort{
				{
					Name:       "udp-edit",
					Protocol:   v1.ProtocolUDP,
					Port:       65000,
					TargetPort: intstr.IntOrString{IntVal: 12345},
					NodePort:   30020,
				},
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())
		})

		It("create service for update of infra", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test2",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30002,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("get infrastructure from LB")
			var infraValue *yawolv1beta1.LoadBalancerInfrastructure
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test2", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Ports == nil || lb.Spec.Endpoints == nil {
					return helper.ErrWaitingForPortsAndEndpoints
				}
				infraValue = lb.Spec.Infrastructure.DeepCopy()
				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("change infrastructure")

			patch := []byte(`{"spec":{` +
				`"infrastructure":{` +
				`"defaultNetwork":{` +
				`"floatingNetID":"CHANGED",` +
				`"networkID":"CHANGED"},` +
				`"availabilityZone":"CHANGED",` +
				`"flavor":{"flavor_id":"CHANGED"},` +
				`"image":{"image_id":"CHANGED"},` +
				`"authSecretRef":{}}}}`)
			Expect(k8sClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))).Should(Succeed())

			By("change for reconcile")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.Spec.Ports = []v1.ServicePort{
				{
					Name:       "port1-edit",
					Protocol:   v1.ProtocolTCP,
					Port:       65000,
					TargetPort: intstr.IntOrString{IntVal: 12345},
					NodePort:   30002,
				},
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check for infrastructure update")
			_ = infraValue
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test2", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.DefaultNetwork.NetworkID == *testInfraDefaults.NetworkID &&
					*lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID == *testInfraDefaults.FloatingNetworkID &&
					*lb.Spec.Infrastructure.Flavor.FlavorID == *testInfraDefaults.FlavorRef.FlavorID &&
					*lb.Spec.Infrastructure.Image.ImageID == *testInfraDefaults.ImageRef.ImageID {
					return nil
				}
				return fmt.Errorf("infrastructure is not updated: %v", lb.Spec.Infrastructure)
			}, time.Second*15, time.Millisecond*500).Should(Succeed())
		})

		It("create service with source ranges", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test20",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					LoadBalancerSourceRanges: []string{
						"1.1.1.1/24",
					},
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30034,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check ranges in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "default--service-test20", Namespace: service.Namespace,
				}, &lb)

				if err != nil {
					return err
				}

				if lb.Spec.Options.LoadBalancerSourceRanges == nil || len(lb.Spec.Options.LoadBalancerSourceRanges) != 1 {
					return helper.ErrZeroOrMoreThanOneSRFoundInLB
				}

				if lb.Spec.Options.LoadBalancerSourceRanges[0] == "1.1.1.1/24" {
					return nil
				}

				return helper.ErrSourceRangesAreWrong
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for creation")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}

				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == service.Name &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer is in creation") {
						return nil
					}
				}

				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("change for reconcile")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name: service.Name, Namespace: service.Namespace,
			}, &service)).Should(Succeed())

			service.Spec.LoadBalancerSourceRanges = []string{
				"1.1.1.1/24",
				"2.2.2.2/16",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "default--service-test20", Namespace: service.Namespace,
				}, &lb)

				if err != nil {
					return err
				}

				if lb.Spec.Options.LoadBalancerSourceRanges == nil || len(lb.Spec.Options.LoadBalancerSourceRanges) != 2 {
					return helper.ErrSourceRangesWrongLength
				}

				if lb.Spec.Options.LoadBalancerSourceRanges[0] == "1.1.1.1/24" &&
					lb.Spec.Options.LoadBalancerSourceRanges[1] == "2.2.2.2/16" {
					return nil
				}

				return helper.ErrSourceRangesAreWrong
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("remove LoadBalancerSourceRange")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name: service.Name, Namespace: service.Namespace,
			}, &service)).Should(Succeed())

			service.Spec.LoadBalancerSourceRanges = []string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "default--service-test20", Namespace: service.Namespace,
				}, &lb)

				if err != nil {
					return err
				}

				if lb.Spec.Options.LoadBalancerSourceRanges != nil {
					return helper.ErrSourceRangesWrongLength
				}

				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("add LoadBalancerSourceRanges via annotation")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name: service.Name, Namespace: service.Namespace,
			}, &service)).Should(Succeed())
			if service.Annotations == nil {
				service.Annotations = make(map[string]string)
			}
			service.Annotations[yawolv1beta1.ServiceLoadBalancerSourceRanges] = "2.2.2.2/24,4.2.2.2/16"
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "default--service-test20", Namespace: service.Namespace,
				}, &lb)

				if err != nil {
					return err
				}

				if lb.Spec.Options.LoadBalancerSourceRanges == nil || len(lb.Spec.Options.LoadBalancerSourceRanges) != 2 {
					return helper.ErrSourceRangesWrongLength
				}

				if lb.Spec.Options.LoadBalancerSourceRanges[0] == "2.2.2.2/24" &&
					lb.Spec.Options.LoadBalancerSourceRanges[1] == "4.2.2.2/16" {
					return nil
				}

				return helper.ErrSourceRangesAreWrong
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("clean up load balancer")
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "default--service-test20", Namespace: "default",
				}, &lb)

				if err != nil {
					return client.IgnoreNotFound(err)
				}

				return helper.ErrLBNotCleanedUp
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with wrong protocol", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test3",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolSCTP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30003,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check for protocol in ports")
			Consistently(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test3", Namespace: "default"}, &lb)
				if err != nil {
					return client.IgnoreNotFound(err)
				}
				return helper.ErrInvalidProtocol
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("check for event on service")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test3" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "unsupported protocol") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with unsupported option", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test35",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					AllocateLoadBalancerNodePorts: ptr.To(false),
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
			By("check that no lb object got created")
			Consistently(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test35", Namespace: "default"}, &lb)
				if err != nil {
					return client.IgnoreNotFound(err)
				}
				return helper.ErrUnsupportedServiceOption
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("check for event on service")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test35" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "unsupported service option") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with wrong className in annotation", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-name-service-test1",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceClassName: "foo",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65030,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30033,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check for LB creation")
			Consistently(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--class-name-service-test1", Namespace: "default"}, &lb)
				if err != nil {
					return client.IgnoreNotFound(err)
				}
				return helper.ErrInvalidClassname
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with correct classname in annotation", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-name-service-test2",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceClassName: "",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30333,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check creation of LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "default--class-name-service-test2",
					Namespace: "default",
				}, &lb)
				return err
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for creation")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "class-name-service-test2" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer is in creation") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with wrong className in spec", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-name-service-test3",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					LoadBalancerClass: ptr.To("foo"),
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65030,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30133,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check for LB creation")
			Consistently(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--class-name-service-test1", Namespace: "default"}, &lb)
				if err != nil {
					return client.IgnoreNotFound(err)
				}
				return helper.ErrInvalidClassname
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with correct classname in spec", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-name-service-test4",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					LoadBalancerClass: ptr.To(helper.DefaultLoadbalancerClass),
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30335,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check creation of LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "default--class-name-service-test4",
					Namespace: "default",
				}, &lb)
				return err
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for creation")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "class-name-service-test4" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer is in creation") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service without classname", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "class-name-service-test5",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30360,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check creation of LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "default--class-name-service-test5",
					Namespace: "default",
				}, &lb)
				return err
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for creation")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "class-name-service-test5" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer is in creation") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with existingFloatingIP", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test4",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceExistingFloatingIP: "123.123.123.123",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30004,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("update service status with loadbalancer IP")
			tries, maxTries := 0, 20
			var err error = nil
			for {
				if tries++; tries > maxTries {
					err = helper.ErrMaxTriesExceeded
					break
				}

				service.Status = v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "123.123.123.123"},
						},
					},
				}

				err = k8sClient.Status().Update(ctx, &service)
				if err == nil {
					break
				}

				if k8sErrors.IsNotFound(err) {
					continue
				}

				if !k8sErrors.IsConflict(err) {
					break
				}

				// local and remote resources are in conflict
				// update local ones and try again
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), &service)
				if err != nil {
					break
				}
			}

			Expect(err).Should(Succeed())

			By("check for existingFloatingIP in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "default--service-test4", Namespace: "default",
				}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.ExistingFloatingIP != nil && *lb.Spec.ExistingFloatingIP == "123.123.123.123" {
					return nil
				}
				return helper.ErrNoExistingFIP
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with set loadbalancer IP in status and set a different existingFloatingIP", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test5",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceExistingFloatingIP: "124.124.124.124",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30005,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("update service status with loadbalancer IP")
			tries, maxTries := 0, 20
			var err error = nil
			for {
				if tries++; tries > maxTries {
					err = helper.ErrMaxTriesExceeded
					break
				}

				service.Status = v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "123.123.123.123"},
						},
					},
				}

				err = k8sClient.Status().Update(ctx, &service)
				if err == nil {
					break
				}

				if k8sErrors.IsNotFound(err) {
					continue
				}

				if !k8sErrors.IsConflict(err) {
					break
				}

				// local and remote resources are in conflict
				// update local ones and try again
				err = k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), &service)
				if err != nil {
					break
				}
			}

			Expect(err).Should(Succeed())
			By("Check Event for creation")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test5" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "ExistingFloatingIP is not supported") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

		})

		It("Check IPfamily v4", func() {
			By("create ipv6 node")
			nodev6 := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nodev6",
					Namespace: "default",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:               v1.NodeReady,
							Status:             v1.ConditionTrue,
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Reason:             "Ready",
							Message:            "Ready",
						},
					},
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "2001:16b8:3015:1100::1b14",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &nodev6)).Should(Succeed())

			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test6",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30006,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test6", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				for _, port := range lb.Spec.Endpoints {
					for _, addr := range port.Addresses {
						ip := net.ParseIP(addr)
						if ip == nil {
							return fmt.Errorf("%w: %v", helper.ErrNotAValidIP, addr)

						}
						if strings.Contains(addr, ":") {
							return fmt.Errorf("no ipv4: %v", lb.Spec.Endpoints)
						}
					}
				}
				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("delete ipv6 node")
			Expect(k8sClient.Delete(ctx, &nodev6)).Should(Succeed())
		})

		It("Check IPfamily v6", func() {
			By("create ipv4 node")
			nodev4 := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nodev4",
					Namespace: "default",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:               v1.NodeReady,
							Status:             v1.ConditionTrue,
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Reason:             "Ready",
							Message:            "Ready",
						},
					},
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "127.0.0.1",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &nodev4)).Should(Succeed())

			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test7",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					IPFamilies: []v1.IPFamily{v1.IPv6Protocol},
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30007,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test7", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				for _, port := range lb.Spec.Endpoints {
					for _, addr := range port.Addresses {
						ip := net.ParseIP(addr)
						if ip == nil {
							return fmt.Errorf("%w: %v", helper.ErrNotAValidIP, addr)
						}
						if strings.Contains(addr, ".") {
							return fmt.Errorf("no ipv6: %v service: %v", lb.Spec.Endpoints, service.Spec.IPFamilies)
						}
					}
				}
				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("delete ipv4 node")
			Expect(k8sClient.Delete(ctx, &nodev4)).Should(Succeed())
		})

		It("Check DualStack", func() {
			By("create ipv4 node")
			nodev4 := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nodev4",
					Namespace: "default",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:               v1.NodeReady,
							Status:             v1.ConditionTrue,
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Reason:             "Ready",
							Message:            "Ready",
						},
					},
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "127.0.0.1",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &nodev4)).Should(Succeed())

			By("create ipv6 node")
			nodev6 := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nodev6",
					Namespace: "default",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:               v1.NodeReady,
							Status:             v1.ConditionTrue,
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Reason:             "Ready",
							Message:            "Ready",
						},
					},
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "2001:16b8:3015:1100::1b14",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &nodev6)).Should(Succeed())

			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test13",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					IPFamilies:     []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
					IPFamilyPolicy: (*v1.IPFamilyPolicyType)(ptr.To(string(v1.IPFamilyPolicyRequireDualStack))),
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30077,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test13", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}

				ipv4Node, ipv6Node := false, false

				for _, port := range lb.Spec.Endpoints {
					for _, addr := range port.Addresses {
						ip := net.ParseIP(addr)
						if ip == nil {
							return fmt.Errorf("%w: %v", helper.ErrNotAValidIP, addr)
						}

						if strings.Contains(addr, ".") {
							ipv4Node = true
							continue
						}

						if strings.Contains(addr, ":") {
							ipv6Node = true
							continue
						}
					}
				}

				if !ipv4Node || !ipv6Node {
					return helper.ErrLBOnlyOneIPFamily
				}

				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("cleanup nodes")
			Expect(k8sClient.Delete(ctx, &nodev4)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &nodev6)).Should(Succeed())
		})

		It("Check log names", func() {
			By("create ns with long name")

			ns := v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-fgjrbquqfcgwpqftfppyapknhxupzzreturfdvgxqjnzrtjfjhsafksghump"},
			}
			Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test-8xydkuayhfunmrtfqrbrmkkppeudddhhzrzymbreqntsxagrmy",
					Namespace: ns.Name},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       65000,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30008,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: service.Namespace + "--" + service.Name, Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

		})

		It("create service and check debugsettings", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test9",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceDebug:       "true",
						yawolv1beta1.ServiceDebugSSHKey: "sshkey",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30009,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check debug settings in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test9", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.DebugSettings.Enabled &&
					lb.Spec.DebugSettings.SshkeyName == "sshkey" {
					return nil
				}
				return fmt.Errorf("wrong debug settings %v", lb.Spec.DebugSettings)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service and check debugsettings patch", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test10",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceDebugSSHKey: "sshkey",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30010,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check debug settings in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test10", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if !lb.Spec.DebugSettings.Enabled {
					return nil
				}
				return fmt.Errorf("debug settings are enabled %v", lb.Spec.DebugSettings)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and enable debug")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDebug:       "true",
				yawolv1beta1.ServiceDebugSSHKey: "sshkey",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new debug settings")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test10", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.DebugSettings.Enabled &&
					lb.Spec.DebugSettings.SshkeyName == "sshkey" {
					return nil
				}
				return fmt.Errorf("wrong debug settings %v", lb.Spec.DebugSettings)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to new sshkeyname")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDebug:       "true",
				yawolv1beta1.ServiceDebugSSHKey: "sshkeynew",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new debug settings")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test10", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.DebugSettings.Enabled &&
					lb.Spec.DebugSettings.SshkeyName == "sshkeynew" {
					return nil
				}
				return fmt.Errorf("wrong debug settings %v", lb.Spec.DebugSettings)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and disable debug")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check debug settings in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test10", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if !lb.Spec.DebugSettings.Enabled {
					return nil
				}
				return fmt.Errorf("debug settings are enabled %v", lb.Spec.DebugSettings)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should create a service and update replicas", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test16",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30210,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking replicas in loadbalancer")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test16", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Replicas == 1 {
					return nil
				}
				return fmt.Errorf("load balancer replicas are wrong: %v", lb.Spec.Replicas)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and change replicas")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceReplicas: "3",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new replicas")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test16", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Replicas == 3 {
					return nil
				}
				return fmt.Errorf("wrong replicas in lb %v", lb.Spec.Replicas)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service and overwrite infra defaults", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test11",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceFlavorID:             "ServiceFlavorID",
						yawolv1beta1.ServiceImageID:              "ServiceImageID",
						yawolv1beta1.ServiceInternalLoadbalancer: "1",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30011,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check infraDefaults overwrite")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test11", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.Flavor.FlavorID == nil ||
					*lb.Spec.Infrastructure.Flavor.FlavorID != service.Annotations[yawolv1beta1.ServiceFlavorID] {
					return fmt.Errorf("wrong infraDefaults flavor id %v", lb.Spec.Infrastructure)
				}
				if lb.Spec.Infrastructure.Image.ImageID == nil ||
					*lb.Spec.Infrastructure.Image.ImageID != service.Annotations[yawolv1beta1.ServiceImageID] {
					return fmt.Errorf("wrong infraDefaults image id %v", lb.Spec.Infrastructure)
				}
				if !lb.Spec.Options.InternalLB {
					return fmt.Errorf("wrong infraDefaults internalLB %v", lb.Spec.Options.InternalLB)
				}
				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service and check infra defaults patch", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test12",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30012,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check infra defaults settings in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test12", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}

				if lb.Spec.Infrastructure.Flavor.FlavorID == nil ||
					*lb.Spec.Infrastructure.Flavor.FlavorID != *testInfraDefaults.FlavorRef.FlavorID {
					return fmt.Errorf("wrong infraDefaults flavor id %v", lb.Spec.Infrastructure)
				}
				if lb.Spec.Infrastructure.Image.ImageID == nil ||
					*lb.Spec.Infrastructure.Image.ImageID != *testInfraDefaults.ImageRef.ImageID {
					return fmt.Errorf("wrong infraDefaults image id %v", lb.Spec.Infrastructure)
				}
				if lb.Spec.Options.InternalLB != *testInfraDefaults.InternalLB {
					return fmt.Errorf("wrong infraDefaults internalLB %v", lb.Spec.Options.InternalLB)
				}

				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and change infra defaults")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceFlavorID:             "ServiceFlavorID",
				yawolv1beta1.ServiceImageID:              "ServiceImageID",
				yawolv1beta1.ServiceInternalLoadbalancer: "true",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets infra defaults settings")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test12", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.Flavor.FlavorID == nil ||
					*lb.Spec.Infrastructure.Flavor.FlavorID != service.Annotations[yawolv1beta1.ServiceFlavorID] {
					return fmt.Errorf("wrong infraDefaults flavor id %v", lb.Spec.Infrastructure)
				}
				if lb.Spec.Infrastructure.Image.ImageID == nil ||
					*lb.Spec.Infrastructure.Image.ImageID != service.Annotations[yawolv1beta1.ServiceImageID] {
					return fmt.Errorf("wrong infraDefaults image id %v", lb.Spec.Infrastructure)
				}
				if !lb.Spec.Options.InternalLB {
					return fmt.Errorf("wrong infraDefaults internalLB %v", lb.Spec.Options.InternalLB)
				}
				return nil
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

		})

		It("create service of type cluster ip and load balancer and await deletion of load balancer", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "service-test19",
					Namespace:   "default",
					Annotations: map[string]string{}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30512,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check LB exists")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test19", Namespace: "default"}, &lb)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("change type of svc to cluster ip")
			patchPayload := []byte(`
[
	{"op":"replace", "path":"/spec/type", "value": "ClusterIP"},
	{"op":"remove", "path":"/spec/ports/0/nodePort"}
]`)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			Eventually(func() error {
				return k8sClient.Patch(ctx, &service, client.RawPatch(types.JSONPatchType, patchPayload))
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("expect deletion of load balancer")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test19", Namespace: "default"}, &lb)
				if err == nil {
					if lb.DeletionTimestamp == nil {
						return fmt.Errorf("loadbalancer still exists and has not been deleted")
					}
					return fmt.Errorf("loadbalancer still exists")
				}
				return client.IgnoreNotFound(err)
			}, time.Second*10, time.Millisecond*500).Should(Succeed())

			By("expect removal of finalizer on service")
			Eventually(func() error {
				var svc v1.Service
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "service-test19",
					Namespace: "default",
				}, &svc)
				if err != nil {
					return err
				}
				for _, finalizer := range svc.Finalizers {
					if finalizer == ServiceFinalizer {
						return fmt.Errorf("finalizer still exists %s/%s:%v", svc.Namespace, svc.Name, svc.Finalizers)
					}
				}
				return nil
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})

		It("should create a service and check TCPProxyProtocol options", func() {
			By("creating a service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test14",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceTCPProxyProtocol: "true",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30014,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check tcpProxyProtocol in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test14", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.TCPProxyProtocol {
					return nil
				}
				return fmt.Errorf("wrong options TCPProxyProtocol %v", lb.Spec.Options.TCPProxyProtocol)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the TCPProxyProtocol field", func() {
			By("creating a service without tcpProxyProtocol")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test17",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30017,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that tcpProxyProtocol is set to false")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test17", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if !lb.Spec.Options.TCPProxyProtocol {
					return nil
				}
				return fmt.Errorf("TCPProxyProtocol is enabled %v", lb.Spec.Options.TCPProxyProtocol)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and enable TCPProxyProtocol")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceTCPProxyProtocol: "true",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for TCPProxyProtocol")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test17", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.TCPProxyProtocol {
					return nil
				}
				return fmt.Errorf("wrong TCPProxyProtocol settings %v", lb.Spec.Options.TCPProxyProtocol)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and enable TCPProxyProtocol")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceTCPProxyProtocol: "false",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for TCPProxyProtocol")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test17", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if !lb.Spec.Options.TCPProxyProtocol {
					return nil
				}
				return fmt.Errorf("wrong TCPProxyProtocol settings %v", lb.Spec.Options.TCPProxyProtocol)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should set the logforward option", func() {
			By("creating the service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test33",
					Namespace: "default",
					Annotations: map[string]string{
						"yawol.stackit.cloud/logForward":  "true",
						"logging.yawol.stackit.cloud/env": "testing",
						"logging.yawol.stackit.cloud/foo": "bar",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30533,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check LB exists")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test33", Namespace: "default"}, &lb)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(lb.Spec.Options.LogForward.Labels["foo"]).To(Equal("bar"))
				g.Expect(lb.Spec.Options.LogForward.Labels["env"]).To(Equal("testing"))
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with classname and load balancer and await deletion of load balancer", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "service-test18",
					Namespace:   "default",
					Annotations: map[string]string{}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30518,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check LB exists")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test18", Namespace: "default"}, &lb)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("change classname")
			patchPayload := []byte(
				`[{"op":"add", "path":"/metadata/annotations", "value": {"yawol.stackit.cloud/className":"wrongclassname"}}]`,
			)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			Eventually(func() error {
				return k8sClient.Patch(ctx, &service, client.RawPatch(types.JSONPatchType, patchPayload))
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("expect deletion of load balancer")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test18", Namespace: "default"}, &lb)
				if err == nil {
					if lb.DeletionTimestamp == nil {
						return fmt.Errorf("loadbalancer still exists and has not been deleted")
					}
					return fmt.Errorf("loadbalancer still exists")
				}
				return client.IgnoreNotFound(err)
			}, time.Second*10, time.Millisecond*500).Should(Succeed())

			By("expect keep of finalizer on service (other controller would add it anyway and it would create an infinite loop)")
			Eventually(func() error {
				var svc v1.Service
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "service-test18",
					Namespace: "default",
				}, &svc)
				if err != nil {
					return err
				}
				for _, finalizer := range svc.Finalizers {
					if finalizer == ServiceFinalizer {
						return nil
					}
				}
				return fmt.Errorf("finalizer is not existing %s/%s:%v", svc.Namespace, svc.Name, svc.Finalizers)
			}, time.Second*20, time.Millisecond*500).Should(Succeed())
		})

		It("should create a service and check TCPIdleTimeout options", func() {
			By("creating a service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test21",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceTCPIdleTimeout: "300s",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30021,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check TCPIdleTimeout in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test21", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.TCPIdleTimeout != nil && lb.Spec.Options.TCPIdleTimeout.Seconds() == float64(300) {
					return nil
				}
				return fmt.Errorf("wrong options TCPIdleTimeout %v", lb.Spec.Options.TCPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the TCPIdleTimeout field", func() {
			By("creating a service without TCPIdleTimeout")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test22",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31022,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that TCPIdleTimeout is set to nil")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test22", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.TCPIdleTimeout == nil {
					return nil
				}
				return fmt.Errorf("TCPIdleTimeout should be nil %v", lb.Spec.Options.TCPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and enable TCPIdleTimeout")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceTCPIdleTimeout: "301s",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for TCPIdleTimeout")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test22", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.TCPIdleTimeout != nil && lb.Spec.Options.TCPIdleTimeout.Seconds() == float64(301) {
					return nil
				}
				return fmt.Errorf("wrong TCPIdleTimeout settings %v", lb.Spec.Options.TCPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and disable TCPIdleTimeout")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for TCPIdleTimeout")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test22", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.TCPIdleTimeout == nil {
					return nil
				}
				return fmt.Errorf("wrong TCPIdleTimeout is not nil %v", lb.Spec.Options.TCPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should create a service and check UDPIdleTimeout options", func() {
			By("creating a service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test23",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceUDPIdleTimeout: "5m",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30023,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check UDPIdleTimeout in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test23", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.UDPIdleTimeout != nil && lb.Spec.Options.UDPIdleTimeout.Seconds() == float64(300) {
					return nil
				}
				return fmt.Errorf("wrong options UDPIdleTimeout %v", lb.Spec.Options.UDPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the UDPIdleTimeout field", func() {
			By("creating a service without UDPIdleTimeout")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test24",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31024,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that UDPIdleTimeout is set to nil")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test24", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.UDPIdleTimeout == nil {
					return nil
				}
				return fmt.Errorf("UDPIdleTimeout should be nil %v", lb.Spec.Options.UDPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and enable UDPIdleTimeout")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceUDPIdleTimeout: "5m1s",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for UDPIdleTimeout")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test24", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.UDPIdleTimeout != nil && lb.Spec.Options.UDPIdleTimeout.Seconds() == float64(301) {
					return nil
				}
				return fmt.Errorf("wrong UDPIdleTimeout settings %v", lb.Spec.Options.UDPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and disable UDPIdleTimeout")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for UDPIdleTimeout")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test24", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.UDPIdleTimeout == nil {
					return nil
				}
				return fmt.Errorf("wrong UDPIdleTimeout is not nil %v", lb.Spec.Options.UDPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create a service with invalid UDPIdleTimeout options", func() {
			By("creating a service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test25",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceUDPIdleTimeout: "300",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30025,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check UDPIdleTimeout in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test25", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.UDPIdleTimeout == nil {
					return nil
				}
				return fmt.Errorf("wrong options UDPIdleTimeout %v", lb.Spec.Options.UDPIdleTimeout)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("Check Event for parse error")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test25" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, yawolv1beta1.ServiceUDPIdleTimeout) {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

		})

		It("should create a service and check ServerGroupPolicy options", func() {
			By("creating a service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test26",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceServerGroupPolicy: "affinity",
					}},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30026,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("check ServerGroupPolicy in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test26", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.ServerGroupPolicy == "affinity" {
					return nil
				}
				return fmt.Errorf("wrong options ServerGroupPolicy %v", lb.Spec.Options.ServerGroupPolicy)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the ServerGroupPolicy field", func() {
			By("creating a service without ServerGroupPolicy")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test27",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31027,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that ServerGroupPolicy is set to empty")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test27", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.ServerGroupPolicy == "" {
					return nil
				}
				return fmt.Errorf("ServerGroupPolicy should be empty %v", lb.Spec.Options.ServerGroupPolicy)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and enable ServerGroupPolicy")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceServerGroupPolicy: "affinity",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for ServerGroupPolicy")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test27", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.ServerGroupPolicy == "affinity" {
					return nil
				}
				return fmt.Errorf("wrong ServerGroupPolicy settings %v", lb.Spec.Options.ServerGroupPolicy)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc and disable ServerGroupPolicy")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new options for ServerGroupPolicy")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test27", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Options.ServerGroupPolicy == "" {
					return nil
				}
				return fmt.Errorf("wrong ServerGroupPolicy is not nil %v", lb.Spec.Options.ServerGroupPolicy)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the floatingNetwork field", func() {
			By("creating a service without floatingNetwork")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test28",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31028,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that the floatingNetwork ID are set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test28", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if *lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID == *testInfraDefaults.FloatingNetworkID {
					return nil
				}
				return fmt.Errorf("floatingNetwork ID is not correct %v", lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to overwrite floatingNetwork ID")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceFloatingNetworkID: "newFloatingID",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new floatingNetwork ID")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test28", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if *lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID == "newFloatingID" {
					return nil
				}
				return fmt.Errorf("floatingNetwork ID is not correct %v", lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to disable overwrite floatingNetwork ID")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("checking that the defaultNetwork ID are set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test28", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if *lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID == *testInfraDefaults.FloatingNetworkID {
					return nil
				}
				return fmt.Errorf("floatingNetwork ID is not correct %v", lb.Spec.Infrastructure.DefaultNetwork.FloatingNetID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the defaultNetwork field", func() {
			By("creating a service without overwritten defaultNetwork")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test29",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31029,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that the defaultNetwork ID are set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test29", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.DefaultNetwork.NetworkID == *testInfraDefaults.NetworkID {
					return nil
				}
				return fmt.Errorf("defaultNetwork IDs are not correct %v", lb.Spec.Infrastructure.DefaultNetwork.NetworkID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to overwrite default network IDs")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDefaultNetworkID: "newNetworkID",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new defaultNetwork IDs")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test29", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.DefaultNetwork.NetworkID == "newNetworkID" &&
					len(lb.Spec.Infrastructure.AdditionalNetworks) == 1 &&
					lb.Spec.Infrastructure.AdditionalNetworks[0].NetworkID == *testInfraDefaults.NetworkID {
					return nil
				}
				return fmt.Errorf("defaultNetwork ID is not correct %v or not infraDefault is not present in additionalNetworks %v",
					lb.Spec.Infrastructure.DefaultNetwork.NetworkID, lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to disable overwrite default network ID")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("checking that the defaultNetwork ID are set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test29", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.DefaultNetwork.NetworkID == *testInfraDefaults.NetworkID &&
					len(lb.Spec.Infrastructure.AdditionalNetworks) == 0 {
					return nil
				}
				return fmt.Errorf("defaultNetwork ID are not correct %v  or still present in additionalNetworks%v",
					lb.Spec.Infrastructure.DefaultNetwork.NetworkID, lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the additionalNetwork field", func() {
			By("creating a service without additionalNetwork")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test30",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31030,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that the additionalNetwork are not set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test30", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if len(lb.Spec.Infrastructure.AdditionalNetworks) == 0 {
					return nil
				}
				return fmt.Errorf("additionalNetwork are already set %v", lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to add additionalNetwork")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceAdditionalNetworks: "additionalNetworkID1,additionalNetworkID1,additionalNetworkID2",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets additionalNetworks")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test30", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if len(lb.Spec.Infrastructure.AdditionalNetworks) == 2 {
					return nil
				}
				return fmt.Errorf("additionalNetworks are not correct %v", lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to disable additionalNetworks")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("checking that no additionalNetworks are set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test30", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if len(lb.Spec.Infrastructure.AdditionalNetworks) == 0 {
					return nil
				}
				return fmt.Errorf("additionalNetwork are still set %v", lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update the skipCloudControllerDefaultNetworkID field", func() {
			By("creating a service without skipCloudControllerDefaultNetworkID")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test31",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceDefaultNetworkID: "default-networkID",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31031,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that defaultNetworkID is in additional networks")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test31", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if len(lb.Spec.Infrastructure.AdditionalNetworks) == 1 &&
					lb.Spec.Infrastructure.AdditionalNetworks[0].NetworkID == *testInfraDefaults.NetworkID {
					return nil
				}
				return fmt.Errorf("additionalNetwork is not set with defaultNetwork %v", lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to add skipCloudControllerDefaultNetworkID")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDefaultNetworkID:                    "default-networkID",
				yawolv1beta1.ServiceSkipCloudControllerDefaultNetworkID: "true",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if default network is not present in additionalNetworks")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test31", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if len(lb.Spec.Infrastructure.AdditionalNetworks) == 0 {
					return nil
				}
				return fmt.Errorf("additionalNetworks are not correct %v", lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to disable skipCloudControllerDefaultNetworkID")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDefaultNetworkID: "default-networkID",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("checking that defaultNetworkID is in additional networks")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test31", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if len(lb.Spec.Infrastructure.AdditionalNetworks) == 1 &&
					lb.Spec.Infrastructure.AdditionalNetworks[0].NetworkID == *testInfraDefaults.NetworkID {
					return nil
				}
				return fmt.Errorf("additionalNetwork is not set with defaultNetwork %v", lb.Spec.Infrastructure.AdditionalNetworks)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should be possible to add a project - but it is immutable", func() {
			By("creating a service with different project")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test32",
					Namespace: "default",
					Annotations: map[string]string{
						yawolv1beta1.ServiceDefaultProjectID: "otherProject",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31032,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that projectID is set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test32", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.ProjectID != nil &&
					*lb.Spec.Infrastructure.ProjectID == "otherProject" {
					return nil
				}
				return fmt.Errorf("projectID is not set in infrastructure %v", lb.Spec.Infrastructure.ProjectID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc with other project - should not work")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDefaultProjectID: "otherProject2",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check for error event")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test32" &&
						event.InvolvedObject.Kind == "Service" &&
						event.Type == v1.EventTypeWarning &&
						strings.Contains(event.Message, helper.ErrProjectIsImmutable.Error()) {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("checking that projectID is still the ID from the creation")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test32", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.ProjectID != nil &&
					*lb.Spec.Infrastructure.ProjectID == "otherProject" {
					return nil
				}
				return fmt.Errorf("projectID is not correctly set in infrastructure %v", lb.Spec.Infrastructure.ProjectID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("should update subnet from annotation", func() {
			By("creating a service without overwritten subnet id")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test34",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31034,
						},
					},
					Type: "LoadBalancer",
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			By("checking that the defaultNetwork SubnetID is set")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test34", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if (testInfraDefaults.SubnetID == nil && lb.Spec.Infrastructure.DefaultNetwork.SubnetID == nil) ||
					(lb.Spec.Infrastructure.DefaultNetwork.SubnetID != nil && testInfraDefaults.SubnetID != nil &&
						*lb.Spec.Infrastructure.DefaultNetwork.SubnetID == *testInfraDefaults.SubnetID) {
					return nil
				}
				return fmt.Errorf("defaultNetwork subbnetID is not correct %v", lb.Spec.Infrastructure.DefaultNetwork.SubnetID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			By("update svc to overwrite subbnetwork ID")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &service)).Should(Succeed())
			service.ObjectMeta.Annotations = map[string]string{
				yawolv1beta1.ServiceDefaultSubnetID: "newSubnetID",
			}
			Expect(k8sClient.Update(ctx, &service)).Should(Succeed())

			By("check if lb gets new subnet ID")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test34", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Infrastructure.DefaultNetwork.SubnetID != nil &&
					*lb.Spec.Infrastructure.DefaultNetwork.SubnetID == "newSubnetID" {
					return nil
				}
				return fmt.Errorf("defaultNetwork SubnetID is not correct %v", lb.Spec.Infrastructure.DefaultNetwork.NetworkID)
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})
	})
})
