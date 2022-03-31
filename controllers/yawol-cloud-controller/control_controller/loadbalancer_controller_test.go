package control_controller

import (
	"context"
	"errors"
	"strings"
	"time"

	"dev.azure.com/schwarzit/schwarzit.ske/yawol.git/controllers/yawol-cloud-controller/target_controller"
	. "github.com/onsi/ginkgo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check loadbalancer reconcile", func() {
	Context("run tests", func() {
		ctx := context.Background()
		var lb yawolv1beta1.LoadBalancer
		var service v1.Service

		It("create service and lb - check external IP and event", func() {
			By("create service")
			service = v1.Service{
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
			replicas := 1
			externalIP := "123.123.123.123"
			lb = yawolv1beta1.LoadBalancer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default--service-test1",
					Namespace: "default",
					Annotations: map[string]string{
						target_controller.ServiceAnnotation: "default/service-test1",
					},
				},
				Spec: yawolv1beta1.LoadBalancerSpec{
					Selector:       metav1.LabelSelector{},
					Replicas:       1,
					ExternalIP:     nil,
					InternalLB:     false,
					Endpoints:      nil,
					Ports:          nil,
					Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{},
				}}
			Expect(k8sClient.Create(ctx, &lb)).Should(Succeed())
			lb.Status = yawolv1beta1.LoadBalancerStatus{
				ReadyReplicas:     &replicas,
				Replicas:          &replicas,
				ExternalIP:        &externalIP,
				FloatingID:        nil,
				FloatingName:      nil,
				PortID:            nil,
				PortName:          nil,
				SecurityGroupID:   nil,
				SecurityGroupName: nil,
				NodeRoleRef:       nil,
			}
			Expect(k8sClient.Status().Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "service-test1", Namespace: "default"}, &service)
				if err != nil {
					return err
				}
				if len(service.Status.LoadBalancer.Ingress) == 1 &&
					service.Status.LoadBalancer.Ingress[0].IP == externalIP {
					return nil
				}
				return errors.New("ip not in status")
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "service-test1" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer is successfully created with IP") {
						return nil
					}
				}
				return errors.New("no event found")
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("create service and lb - not ready LB - no external IP", func() {
			By("create service")
			service = v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-test2",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   30002,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
			replicas := 1
			externalIP := "123.123.123.123"
			lb = yawolv1beta1.LoadBalancer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default--service-test2",
					Namespace: "default",
					Annotations: map[string]string{
						target_controller.ServiceAnnotation: "default/service-test2",
					},
				},
				Spec: yawolv1beta1.LoadBalancerSpec{
					Selector:       metav1.LabelSelector{},
					Replicas:       1,
					ExternalIP:     nil,
					InternalLB:     false,
					Endpoints:      nil,
					Ports:          nil,
					Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{},
				}}
			Expect(k8sClient.Create(ctx, &lb)).Should(Succeed())
			lb.Status = yawolv1beta1.LoadBalancerStatus{
				ReadyReplicas:     nil,
				Replicas:          &replicas,
				ExternalIP:        &externalIP,
				FloatingID:        nil,
				FloatingName:      nil,
				PortID:            nil,
				PortName:          nil,
				SecurityGroupID:   nil,
				SecurityGroupName: nil,
				NodeRoleRef:       nil,
			}
			Expect(k8sClient.Status().Update(ctx, &lb)).Should(Succeed())

			Consistently(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "service-test2", Namespace: "default"}, &service)
				if err != nil {
					return err
				}
				if len(service.Status.LoadBalancer.Ingress) == 0 {
					return nil
				}
				return errors.New("ip is set in status but lb is not ready")
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

			lb.Status.ReadyReplicas = &replicas
			Expect(k8sClient.Status().Update(ctx, &lb)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "service-test2", Namespace: "default"}, &service)
				if err != nil {
					return err
				}
				if len(service.Status.LoadBalancer.Ingress) == 1 &&
					service.Status.LoadBalancer.Ingress[0].IP == externalIP {
					return nil
				}
				return errors.New("ip not in status")
			}, time.Second*5, time.Millisecond*500).Should(Succeed())

		})

	})
})
