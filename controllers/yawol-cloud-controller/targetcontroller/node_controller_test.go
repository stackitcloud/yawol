package targetcontroller

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	predicatesEvent "sigs.k8s.io/controller-runtime/pkg/event"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("check controller-runtime predicate", func() {
	conditionReady := v1.NodeCondition{
		Type:               v1.NodeReady,
		Status:             v1.ConditionTrue,
		LastHeartbeatTime:  metav1.Time{},
		LastTransitionTime: metav1.Time{},
		Reason:             "Ready",
		Message:            "Ready",
	}
	conditionNotReady := v1.NodeCondition{
		Type:               v1.NodeReady,
		Status:             v1.ConditionFalse,
		LastHeartbeatTime:  metav1.Time{},
		LastTransitionTime: metav1.Time{},
		Reason:             "Ready",
		Message:            "Ready",
	}

	baseNode := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node1",
			Namespace: "default"},
		Spec: v1.NodeSpec{},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				conditionReady,
			},
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "10.10.10.10",
				}, {
					Type:    v1.NodeInternalIP,
					Address: "2001:16b8:3015:1100::1b14",
				},
			},
		},
	}

	It("should reconcile", func() {
		By("change in ready condition", func() {
			oldNode := baseNode.DeepCopy()
			newNode := baseNode.DeepCopy()
			newNode.Status.Conditions = []v1.NodeCondition{conditionNotReady}

			event := predicatesEvent.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			}
			Expect(yawolNodePredicate().Update(event)).To(BeTrue())
		})
		By("change in node addresses", func() {
			oldNode := baseNode.DeepCopy()
			newNode := baseNode.DeepCopy()
			newNode.Status.Addresses = append(newNode.Status.Addresses, v1.NodeAddress{
				Type:    v1.NodeInternalIP,
				Address: "10.10.10.11",
			})

			event := predicatesEvent.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			}
			Expect(yawolNodePredicate().Update(event)).To(BeTrue())
		})
		By("create", func() {
			event := predicatesEvent.CreateEvent{
				Object: baseNode.DeepCopy(),
			}
			Expect(yawolNodePredicate().Create(event)).To(BeTrue())
		})
		By("delete", func() {
			event := predicatesEvent.DeleteEvent{
				Object: baseNode.DeepCopy(),
			}
			Expect(yawolNodePredicate().Delete(event)).To(BeTrue())
		})
	})

	It("should not reconcile", func() {
		By("change if no change in ready condition or node addresses", func() {
			event := predicatesEvent.UpdateEvent{
				ObjectOld: baseNode.DeepCopy(),
				ObjectNew: baseNode.DeepCopy(),
			}
			Expect(yawolNodePredicate().Update(event)).To(BeFalse())
		})
		By("on generic event", func() {
			event := predicatesEvent.GenericEvent{
				Object: baseNode.DeepCopy(),
			}
			Expect(yawolNodePredicate().Generic(event)).To(BeFalse())
		})
	})
})

var _ = Describe("Check loadbalancer reconcile", Serial, Ordered, func() {
	Context("run tests", func() {
		ctx := context.Background()
		var lb yawolv1beta1.LoadBalancer

		It("Create service and check without node", func() {
			By("create service")
			service := v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-test1",
					Namespace: "default"},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Protocol: v1.ProtocolTCP,
						Port:     8123,
					}},
					Type: "LoadBalancer",
				}}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--node-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Endpoints == nil || len(lb.Spec.Endpoints) == 0 {
					return nil
				}
				return helper.ErrEndpointsFound
			}, time.Second*15, time.Millisecond*500).Should(Succeed())

		})

		assertLBWithOneEndpoint := func(nodeName string, nodeAddress string) {
			GinkgoHelper()
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--node-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Endpoints == nil || len(lb.Spec.Endpoints) != 1 {
					return fmt.Errorf("no or more than one endpoint in LB found: %v", lb.Spec.Endpoints)
				}
				if len(lb.Spec.Endpoints[0].Addresses) != 1 {
					return fmt.Errorf("no or more than one endpoint address in LB found: %v", lb.Spec.Endpoints[0].Addresses)
				}
				if lb.Spec.Endpoints[0].Name != nodeName || lb.Spec.Endpoints[0].Addresses[0] != nodeAddress {
					return helper.ErrEndpointValuesWrong

				}
				return nil
			}, time.Second*15, time.Millisecond*500).Should(Succeed())
		}

		It("Create node and check node", func() {
			By("create node")
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node1",
					Namespace: "default"},
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
							Address: "10.10.10.10",
						}, {
							Type:    v1.NodeInternalIP,
							Address: "2001:16b8:3015:1100::1b14",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &node)).Should(Succeed())

			By("check node in LB")
			assertLBWithOneEndpoint("node1", "10.10.10.10")
			By("check event for node sync")
			Eventually(func() error {
				eventList := v1.EventList{}
				err := k8sClient.List(ctx, &eventList)
				if err != nil {
					return err
				}
				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == "node-test1" &&
						event.InvolvedObject.Kind == "Service" &&
						strings.Contains(event.Message, "LoadBalancer endpoints successfully synced with nodes addresses") {
						return nil
					}
				}
				return helper.ErrNoEventFound
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})
		It("ignore terminating Node and check", func() {
			By("create node")
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nodebrk",
					Namespace: "default"},
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
						{
							Type:               NodeTerminationCondition,
							Status:             v1.ConditionTrue,
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Reason:             "Terminating",
							Message:            "Terminating",
						},
					},
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "10.10.10.42",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &node)).Should(Succeed())

			By("check node in LB")
			assertLBWithOneEndpoint("node1", "10.10.10.10")
		})
		It("ignore tainted Node and check", func() {
			By("create node")
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nodebrk2",
					Namespace: "default"},
				Spec: v1.NodeSpec{
					Taints: []v1.Taint{
						{
							Key:    ToBeDeletedTaint,
							Effect: v1.TaintEffectNoSchedule,
							Value:  "123456789", // unix timestamp
						},
					},
				},
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
							Address: "10.10.10.43",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &node)).Should(Succeed())

			By("check node in LB")
			assertLBWithOneEndpoint("node1", "10.10.10.10")
		})

		It("add Node and check", func() {
			By("create node")
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node2",
					Namespace: "default"},
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
							Address: "10.10.10.11",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &node)).Should(Succeed())

			By("check node in LB")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--node-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Endpoints == nil || len(lb.Spec.Endpoints) != 2 {
					return fmt.Errorf("less or more than two endpoint in LB found: %v", lb.Spec.Endpoints)
				}
				if len(lb.Spec.Endpoints[0].Addresses) != 1 {
					return fmt.Errorf("no or more than one endpoint0 address in LB found: %v", lb.Spec.Endpoints[0].Addresses)
				}
				if len(lb.Spec.Endpoints[1].Addresses) != 1 {
					return fmt.Errorf("no or more than one endpoint1 address in LB found: %v", lb.Spec.Endpoints[1].Addresses)
				}
				if lb.Spec.Endpoints[0].Name == "node1" &&
					lb.Spec.Endpoints[0].Addresses[0] == "10.10.10.10" &&
					lb.Spec.Endpoints[1].Name == "node2" &&
					lb.Spec.Endpoints[1].Addresses[0] == "10.10.10.11" {
					return nil
				}
				return helper.ErrSourceRangesAreWrong
			}, time.Second*15, time.Millisecond*500).Should(Succeed())
		})
		It("not ready Node and check", func() {
			By("get node count in LB before")
			var nodeCount int
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--node-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Endpoints == nil {
					return helper.ErrNoEndpointFound
				}
				nodeCount = len(lb.Spec.Endpoints)
				return nil
			}, time.Second*15, time.Millisecond*500).Should(Succeed())
			By("get node")
			node := v1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "node2"}, &node)).Should(Succeed())

			By("set node to not ready")
			node.Status.Conditions = []v1.NodeCondition{
				{
					Type:               v1.NodeReady,
					Status:             v1.ConditionFalse,
					LastHeartbeatTime:  metav1.Time{},
					LastTransitionTime: metav1.Time{},
					Reason:             "notready",
					Message:            "notready",
				},
			}
			Expect(k8sClient.Status().Update(ctx, &node)).Should(Succeed())

			By("check node count in LB after not ready")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--node-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Endpoints == nil || len(lb.Spec.Endpoints) != nodeCount-1 {
					return fmt.Errorf("found %v but expect %v endpoint in LB", len(lb.Spec.Endpoints), nodeCount-1)
				}
				return nil
			}, time.Second*15, time.Millisecond*500).Should(Succeed())

			By("set node to ready")
			node.Status.Conditions = []v1.NodeCondition{
				{
					Type:               v1.NodeReady,
					Status:             v1.ConditionTrue,
					LastHeartbeatTime:  metav1.Time{},
					LastTransitionTime: metav1.Time{},
					Reason:             "ready",
					Message:            "ready",
				},
			}
			Expect(k8sClient.Status().Update(ctx, &node)).Should(Succeed())

			By("check node count in LB ready again")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default--node-test1", Namespace: "default"}, &lb)
				if err != nil {
					return err
				}
				if lb.Spec.Endpoints == nil || len(lb.Spec.Endpoints) != nodeCount {
					return helper.ErrEndpointDoesNotMatchingNodeCount
				}
				return nil
			}, time.Second*15, time.Millisecond*500).Should(Succeed())
		})
	})
})
