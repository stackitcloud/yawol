package controlcontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/targetcontroller"
	"k8s.io/apimachinery/pkg/util/intstr"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check loadbalancer reconcile", func() {
	Context("run tests", func() {
		ctx := context.Background()
		var lb yawolv1beta1.LoadBalancer
		var service v1.Service

		It("create service and LB", func() {
			By("create service")
			service = v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-test1",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "port1",
							Protocol:   v1.ProtocolTCP,
							Port:       12345,
							TargetPort: intstr.IntOrString{IntVal: 12345},
							NodePort:   31001,
						},
					},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			lb = yawolv1beta1.LoadBalancer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service.Namespace + "--" + service.Name,
					Namespace: "default",
					Annotations: map[string]string{
						targetcontroller.ServiceAnnotation: service.Namespace + "/" + service.Name,
					},
				},
				Spec: yawolv1beta1.LoadBalancerSpec{
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(ctx, &lb)).Should(Succeed())
			fmt.Println(lb)
		})

		It("create event on LB and check event on service", func() {
			By("create event")
			event := v1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-test1",
					Namespace: "default"},
				InvolvedObject: v1.ObjectReference{
					Kind:       "LoadBalancer",
					Namespace:  lb.Namespace,
					Name:       lb.Name,
					UID:        lb.UID,
					APIVersion: lb.APIVersion,
				},
				Message: "test message",
				Type:    v1.EventTypeNormal,
				Reason:  "reason",
				Source:  v1.EventSource{Component: EventSource},
			}
			Expect(k8sClient.Create(ctx, &event)).Should(Succeed())

			Eventually(func() error {
				var curEvents v1.EventList
				err := k8sClient.List(ctx, &curEvents)
				if err != nil {
					return err
				}
				for _, curEvent := range curEvents.Items {
					if curEvent.InvolvedObject.UID == service.UID {
						if curEvent.Message == event.Message &&
							curEvent.Type == event.Type &&
							curEvent.Reason == event.Reason {
							return nil
						}
						return helper.ErrIncorrectEvent
					}
				}
				return helper.ErrSvcEventNotFound
			}, time.Second*15, time.Millisecond*500).Should(Succeed())
		})
	})
})
