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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check loadbalancer reconcile", func() {
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
				`"floatingNetID":"CHANGED",` +
				`"networkID":"CHANGED",` +
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
				if lb.Spec.Infrastructure.NetworkID == *testInfraDefaults.NetworkID &&
					*lb.Spec.Infrastructure.FloatingNetID == *testInfraDefaults.FloatingNetworkID &&
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

		It("create service with wrong className", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test8",
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
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--service-test8", Namespace: "default"}, &lb)
				if err != nil {
					return client.IgnoreNotFound(err)
				}
				return helper.ErrInvalidClassname
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service with correct classname", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test15",
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
					Name:      "default--service-test15",
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
					if event.InvolvedObject.Name == "service-test15" &&
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
					IPFamilyPolicy: (*v1.IPFamilyPolicyType)(pointer.String(string(v1.IPFamilyPolicyRequireDualStack))),
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
				fmt.Println(err)
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
				fmt.Println(err)
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
	})
})
