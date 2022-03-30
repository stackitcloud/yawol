package loadbalancer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/openstack"
	"github.com/stackitcloud/yawol/internal/openstack/testing"
	v1 "k8s.io/api/core/v1"

	rbac "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type LB = yawolv1beta1.LoadBalancer

const (
	timeout  = time.Second * 30
	interval = time.Millisecond * 250
)

var _ = Describe("loadbalancer controller", func() {
	var (
		lb     *LB
		client *testing.MockClient
	)

	lbName := "testlb"
	lbNN := types.NamespacedName{
		Name:      lbName,
		Namespace: namespace,
	}

	// reset closure variables to a known state
	BeforeEach(func() {
		lb = getMockLB(lbNN)

		client = testing.GetFakeClient()
		loadBalancerReconciler.getOsClientForIni = func(iniData []byte) (openstack.Client, error) {
			return client, nil
		}
	})

	// used to deploy lb into the cluster
	JustBeforeEach(func() {
		loadBalancerReconciler.skipReconciles = true
		Expect(k8sClient.Create(ctx, lb)).Should(Succeed())
		patchStatus(lb, lb.Status)
		loadBalancerReconciler.skipReconciles = false
	})

	AfterEach(func() {
		// cleanup loadbalancer
		cleanupLB(lbNN, timeout)
	})

	It("should have a working before- and aftereach setup", func() {
		Expect(true).To(BeTrue())
	})

	When("everything is set to default", func() {
		It("should create the lb", func() {
			hopefully(lbNN, func(g Gomega, act LB) error {
				if len(act.Finalizers) == 0 {
					return fmt.Errorf("no finalizers on lb")
				}

				return nil
			})
		})
	})

	When("external ip is set", func() {
		BeforeEach(func() {
			lb.Spec.Ports = []v1.ServicePort{{
				Protocol: v1.ProtocolTCP,
				Port:     8080,
				NodePort: 30000,
			}}

			lb.Spec.ExternalIP = pointer.StringPtr("1.1.1.1")
		})

		It("should create openstack resources", func() {
			hopefully(lbNN, func(g Gomega, act LB) error {
				name := lbNN.Namespace + "/" + lbNN.Name

				g.Expect(len(act.Spec.Ports)).To(Equal(1))
				g.Expect(act.Spec.Ports[0].NodePort).To(BeEquivalentTo(30000))

				g.Expect(act.Status).ToNot(BeNil())

				g.Expect(act.Status.PortID).ToNot(BeNil())
				g.Expect(act.Status.PortName).ToNot(BeNil())
				g.Expect(*act.Status.PortName).To(Equal(name))

				g.Expect(act.Status.SecurityGroupID).ToNot(BeNil())
				g.Expect(act.Status.SecurityGroupName).ToNot(BeNil())
				g.Expect(*act.Status.SecurityGroupName).To(Equal(name))

				g.Expect(act.Status.FloatingID).ToNot(BeNil())
				g.Expect(act.Status.FloatingName).ToNot(BeNil())
				g.Expect(*act.Status.FloatingName).To(Equal(name))

				g.Expect(act.Status.ExternalIP).ToNot(BeNil())
				g.Expect(*act.Status.ExternalIP).To(Equal("1.1.1.1"))

				return nil
			})
		})
	})

	When("internal lb is set", func() {
		BeforeEach(func() {
			lb.Spec.Ports = []v1.ServicePort{{
				Protocol: v1.ProtocolTCP,
				Port:     8080,
				NodePort: 30000,
			}}
			lb.Spec.InternalLB = true
		})

		It("should create an internal lb", func() {
			hopefully(lbNN, func(g Gomega, act LB) error {
				g.Expect(act.Status.PortID).ToNot(BeNil())
				g.Expect(act.Status.PortName).ToNot(BeNil())

				g.Expect(act.Status.FloatingID).To(BeNil())
				g.Expect(act.Status.FloatingName).To(BeNil())

				g.Expect(act.Status.ExternalIP).ToNot(BeNil())
				g.Expect(*act.Status.ExternalIP).ToNot(Equal(""))

				return nil
			})
		})
	})

	When("we deploy an external lb", func() {
		It("should swap to an internal lb", func() {
			By("checking that the lb gets created with an public ip")
			hopefully(lbNN, func(g Gomega, act LB) error {
				g.Expect(act.Status.FloatingID).ToNot(BeNil())
				g.Expect(act.Status.FloatingName).ToNot(BeNil())

				g.Expect(act.Status.ExternalIP).ToNot(BeNil())

				return nil
			})

			By("swapping to an internal lb")
			updateLB(lbNN, func(act *LB) {
				act.Spec.InternalLB = true
			})

			hopefully(lbNN, func(g Gomega, act LB) error {
				g.Expect(act.Status.FloatingID).To(BeNil())
				g.Expect(act.Status.FloatingName).To(BeNil())

				g.Expect(act.Status.ExternalIP).ToNot(BeNil())

				return nil
			})

			// TODO hasn't been tested before either
			// By("swapping to an external lb")
			// updateLB(lbNN, func(act *LB) {
			// act.Spec.InternalLB = false
			// })

			// hopefully(lbNN, func(act LB) error {
			// if lb.Status.FloatingID == nil {
			// return fmt.Errorf("floatingid is nil")
			// }

			// if lb.Status.FloatingName == nil {
			// return fmt.Errorf("floatingname is nil")
			// }

			// if lb.Status.ExternalIP == nil {
			// return fmt.Errorf("external ip is not nil")
			// }

			// return nil
			// })
		})
	})

	Context("loadbalancerset", func() {
		It("should create a loadbalancerset", func() {
			hopefully(lbNN, func(g Gomega, act LB) error {
				var lbsetList yawolv1beta1.LoadBalancerSetList
				g.Expect(k8sClient.List(ctx, &lbsetList, &runtimeClient.ListOptions{
					LabelSelector: labels.SelectorFromSet(lb.Spec.Selector.MatchLabels),
				})).Should(Succeed())

				g.Expect(len(lbsetList.Items)).Should(Equal(1))

				// prevent later panic
				g.Expect(act.Status.FloatingID).ShouldNot(BeNil())

				lbset := lbsetList.Items[0]
				g.Expect(lbset.Namespace).Should(Equal(act.Namespace))
				g.Expect(lbset.Name).Should(HavePrefix(act.Name))
				g.Expect(lbset.Spec.Replicas).Should(Equal(1))
				g.Expect(lbset.Spec.Selector.MatchLabels).Should(ContainElement(act.Spec.Selector.MatchLabels["test-label"]))
				g.Expect(lbset.Spec.Template.Labels).Should(ContainElement(act.Spec.Selector.MatchLabels["test-label"]))
				g.Expect(lbset.Spec.Template.Spec.FloatingID).Should(Equal(*act.Status.FloatingID))
				g.Expect(lbset.Spec.Template.Spec.Infrastructure).Should(Equal(act.Spec.Infrastructure))
				g.Expect(lbset.Spec.Template.Spec.LoadBalancerRef.Name).Should(Equal(act.Name))
				g.Expect(lbset.Spec.Template.Spec.LoadBalancerRef.Namespace).Should(Equal(act.Namespace))
				return nil
			})
		})

		It("should create a new lbset when infrastructure changes", func() {
			var hash string
			By("waiting for lbset creation")
			hopefully(lbNN, func(g Gomega, act LB) error {
				var lbsetList yawolv1beta1.LoadBalancerSetList
				g.Expect(k8sClient.List(ctx, &lbsetList, &runtimeClient.ListOptions{
					LabelSelector: labels.SelectorFromSet(lb.Spec.Selector.MatchLabels),
				})).Should(Succeed())

				g.Expect(len(lbsetList.Items)).Should(Equal(1))

				lbset := lbsetList.Items[0]
				split := strings.Split(lbset.Name, "-")
				g.Expect(len(split)).Should(Equal(2))
				hash = split[1]
				return nil
			})

			By("changing flavorid")
			updateLB(lbNN, func(act *LB) {
				act.Spec.Infrastructure.Flavor = &yawolv1beta1.OpenstackFlavorRef{
					FlavorID: pointer.StringPtr("somenewid"),
				}
			})

			By("checking for a new lbset")
			hopefully(lbNN, func(g Gomega, act LB) error {
				var lbsetList yawolv1beta1.LoadBalancerSetList
				g.Expect(k8sClient.List(ctx, &lbsetList, &runtimeClient.ListOptions{
					LabelSelector: labels.SelectorFromSet(lb.Spec.Selector.MatchLabels),
				})).Should(Succeed())

				g.Expect(len(lbsetList.Items)).Should(Equal(2))

				By("testing if the new set got a different hash")
				lbset := lbsetList.Items[1]
				split := strings.Split(lbset.Name, "-")
				g.Expect(len(split)).Should(Equal(2))

				g.Expect(split[1]).ShouldNot(Equal(""))
				g.Expect(hash).ShouldNot(Equal(""))
				g.Expect(hash).ShouldNot(Equal(split[1]))

				By("checking that new lbset is scaled up")
				g.Expect(lbset.Spec.Replicas).To(Equal(act.Spec.Replicas))
				return nil
			})
		})

		It("should up- and downscale loadbalancer machines", func() {
			By("waiting for lb and lbset creation")
			hopefully(lbNN, func(g Gomega, act LB) error {
				var lbsetList yawolv1beta1.LoadBalancerSetList
				g.Expect(k8sClient.List(ctx, &lbsetList, &runtimeClient.ListOptions{
					LabelSelector: labels.SelectorFromSet(lb.Spec.Selector.MatchLabels),
				})).Should(Succeed())

				g.Expect(len(lbsetList.Items)).Should(Equal(1))

				lbset := lbsetList.Items[0]
				g.Expect(lbset.Spec.Replicas).Should(Equal(1))
				return nil
			})

			By("upscaling replicas")
			updateLB(lbNN, func(act *LB) {
				act.Spec.Replicas = 2
			})

			Eventually(func(g Gomega) {
				var lbsetList yawolv1beta1.LoadBalancerSetList
				g.Expect(k8sClient.List(ctx, &lbsetList, &runtimeClient.ListOptions{
					LabelSelector: labels.SelectorFromSet(lb.Spec.Selector.MatchLabels),
				})).Should(Succeed())

				g.Expect(len(lbsetList.Items)).Should(Equal(1))

				lbset := lbsetList.Items[0]
				g.Expect(lbset.Spec.Replicas).Should(Equal(2))
			}, timeout, interval).Should(Succeed())

			By("downscaling replicas")
			updateLB(lbNN, func(act *LB) {
				act.Spec.Replicas = 1
			})

			Eventually(func(g Gomega) {
				var lbsetList yawolv1beta1.LoadBalancerSetList
				g.Expect(k8sClient.List(ctx, &lbsetList, &runtimeClient.ListOptions{
					LabelSelector: labels.SelectorFromSet(lb.Spec.Selector.MatchLabels),
				})).Should(Succeed())

				g.Expect(len(lbsetList.Items)).Should(Equal(1))

				lbset := lbsetList.Items[0]
				g.Expect(lbset.Spec.Replicas).Should(Equal(1))
			}, timeout, interval).Should(Succeed())
		})
	}) // loadbalancerset context

	Context("security group rules", func() {
		BeforeEach(func() {
			lb.Spec.Ports = []v1.ServicePort{
				{Protocol: v1.ProtocolTCP, Port: 8080},
			}
		})

		It("should create matching security group rules", func() {
			By("creating default rules")
			hopefully(lbNN, func(g Gomega, act LB) error {
				if act.Status.SecurityGroupID == nil || act.Status.SecurityGroupName == nil {
					return fmt.Errorf("secgroupid or secgroupname is nil")
				}

				if *act.Status.SecurityGroupName != lbNN.Namespace+"/"+lbNN.Name {
					return fmt.Errorf("secgroupname is wrong: %v", *act.Status.SecurityGroupName)
				}

				rls, err := client.RuleClientObj.List(ctx, rules.ListOpts{
					SecGroupID: *act.Status.SecurityGroupID,
				})

				if err != nil {
					return err
				}

				if len(rls) != len(getDesiredSecGroups(*act.Status.SecurityGroupID))+2 {
					return fmt.Errorf("wrong amount of secgroup rules were applied %v %v", len(rls), lb.Spec.Ports)
				}

				prts, err := client.PortClientObj.List(ctx, ports.ListOpts{})
				g.Expect(err).To(Succeed())
				g.Expect(len(prts)).To(Equal(1))

				return nil
			})

			By("updating ports")
			updateLB(lbNN, func(act *LB) {
				act.Spec.Ports = []v1.ServicePort{
					{Protocol: v1.ProtocolTCP, Port: 8080},
					{Protocol: v1.ProtocolTCP, Port: 8090},
				}
			})

			hopefully(lbNN, func(g Gomega, act LB) error {
				rls, err := client.RuleClientObj.List(ctx, rules.ListOpts{
					SecGroupID: *act.Status.SecurityGroupID,
				})

				if err != nil {
					return err
				}

				if len(rls) != len(getDesiredSecGroups(*act.Status.SecurityGroupID))+4 {
					return fmt.Errorf("wrong amount of secgroup rules were applied %v", len(rls))
				}

				return nil
			})

			By("updating source ranges and udp")
			updateLB(lbNN, func(act *LB) {
				act.Spec.Ports = []v1.ServicePort{
					{Protocol: v1.ProtocolTCP, Port: 8080},
					{Protocol: v1.ProtocolUDP, Port: 8081},
				}
				act.Spec.LoadBalancerSourceRanges = []string{
					"192.168.1.1/24",
					"192.168.2.1/24",
					"192.168.3.1/24",
				}
			})

			hopefully(lbNN, func(g Gomega, act LB) error {
				rls, err := client.RuleClientObj.List(ctx, rules.ListOpts{
					SecGroupID: *act.Status.SecurityGroupID,
				})

				if err != nil {
					return err
				}

				// default rules + len(ports) * len(sourceRanges)
				if len(rls) != len(getDesiredSecGroups(*act.Status.SecurityGroupID))+6 {
					return fmt.Errorf("no additional sec group rules were applied %v", len(rls))
				}

				// test if not all traffic is allowed
				for _, rule := range rls {
					if rule.RemoteIPPrefix == "0.0.0.0/0" ||
						rule.RemoteIPPrefix == "::/0" {
						return fmt.Errorf("all traffic is allowed")
					}
				}

				return nil
			})
		})
	}) // security group rules context

	When("openstack has timeouts", func() {
		reached := false
		BeforeEach(func() {
			client.GroupClientObj = &testing.CallbackGroupClient{
				ListFunc: func(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
					// wait for timeout to finish
					<-ctx.Done()
					reached = true
					err := ctx.Err()
					Expect(err).To(HaveOccurred())
					return nil, err
				},
			}
		})

		It("should not create openstack resources", func() {
			hopefully(lbNN, func(g Gomega, act LB) error {
				g.Expect(reached).To(BeTrue())

				g.Expect(act.Status).ShouldNot(BeNil())
				g.Expect(act.Status.SecurityGroupID).Should(BeNil())
				return nil
			})
		})
	}) // openstack has timeouts

	When("openstack is not working", func() {
		BeforeEach(func() {
			client.GroupClientObj = &testing.CallbackGroupClient{
				ListFunc: func(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
					return []groups.SecGroup{}, gophercloud.ErrDefault401{
						ErrUnexpectedResponseCode: gophercloud.ErrUnexpectedResponseCode{
							Body: []byte("Auth failed"),
						},
					}
				},
				GetFunc: func(ctx context.Context, id string) (*groups.SecGroup, error) {
					return nil, gophercloud.ErrDefault401{
						ErrUnexpectedResponseCode: gophercloud.ErrUnexpectedResponseCode{
							Body: []byte("Auth failed"),
						},
					}
				},
				CreateFunc: func(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error) {
					return nil, gophercloud.ErrDefault401{}
				},
				DeleteFunc: func(ctx context.Context, id string) error {
					return gophercloud.ErrDefault401{}
				},
			}
			client.FipClientObj = &testing.CallbackFipClient{
				GetFunc: func(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
					return nil, gophercloud.ErrDefault403{}
				},
			}
		})

		It("should not create openstack resources", func() {
			By("checking that no OS resource is created")
			hopefully(lbNN, func(g Gomega, act LB) error {
				g.Expect(act.Status).ShouldNot(BeNil())
				g.Expect(act.Status.FloatingID).Should(BeNil())
				g.Expect(act.Status.FloatingName).Should(BeNil())
				g.Expect(act.Status.PortID).Should(BeNil())
				g.Expect(act.Status.PortName).Should(BeNil())
				g.Expect(act.Status.FloatingName).Should(BeNil())
				g.Expect(act.Status.FloatingID).Should(BeNil())
				g.Expect(act.Status.SecurityGroupID).Should(BeNil())
				g.Expect(act.Status.SecurityGroupName).Should(BeNil())
				return nil
			})

			By("checking that the event that credentials are wrong is created")
			Eventually(func() error {
				var events v1.EventList
				err := k8sClient.List(ctx, &events)
				if err != nil {
					return err
				}

				for _, event := range events.Items {
					if event.Reason == "Failed" &&
						event.Type == "Warning" &&
						event.Count > 0 {
						return nil
					}
				}
				return fmt.Errorf("expected event not found")
			}, timeout, interval).Should(Succeed())
		})
	}) // openstack not working context

	When("migrating from octavia", func() {
		var octaviaPortID, yawolPortID string
		var fipFound, fipUnbound, fipRebound, octaviaDeleted bool

		BeforeEach(func() {
			loadBalancerReconciler.MigrateFromOctavia = true
			lb.Spec.ExternalIP = pointer.StringPtr("10.0.0.1")

			octaviaPortID = "44444-55555-66666"
			yawolPortID = "66666-5555-4444"

			fipFound = false
			fipUnbound = false
			fipRebound = false
			octaviaDeleted = false

			client.FipClientObj = &testing.CallbackFipClient{
				GetFunc: func(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
					if !fipUnbound {
						return &floatingips.FloatingIP{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
							PortID:      octaviaPortID,
						}, nil
					}

					if fipUnbound && !fipRebound {
						return &floatingips.FloatingIP{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
						}, nil
					}

					if fipRebound {
						return &floatingips.FloatingIP{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
							PortID:      yawolPortID,
						}, nil
					}
					panic("test should not enter here")
				},
				ListFunc: func(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error) {
					loadBalancerReconciler.Log.Info("listed.fip")
					if !fipUnbound && opts.(floatingips.ListOpts).FloatingIP == "10.0.0.1" {
						fipFound = true
						return []floatingips.FloatingIP{{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
							PortID:      "44444-55555-66666",
						}}, nil
					}
					if fipUnbound && !fipRebound {
						return []floatingips.FloatingIP{{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
						}}, nil
					}
					if opts.(floatingips.ListOpts).PortID == yawolPortID {
						return []floatingips.FloatingIP{
							{
								ID:          "111111-2222222-33333",
								Description: "some random octavia description",
								FloatingIP:  "10.0.0.1",
								PortID:      yawolPortID,
							},
						}, nil
					}

					return nil, nil
				},
				UpdateFunc: func(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error) {
					if id == "111111-2222222-33333" && opts.(floatingips.UpdateOpts).PortID == nil {
						fipUnbound = true
						return &floatingips.FloatingIP{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
						}, nil
					}
					if id == "111111-2222222-33333" && opts.(floatingips.UpdateOpts).PortID != nil &&
						*opts.(floatingips.UpdateOpts).PortID == yawolPortID {
						fipRebound = true
						return &floatingips.FloatingIP{
							ID:          "111111-2222222-33333",
							Description: "some random octavia description",
							FloatingIP:  "10.0.0.1",
							PortID:      yawolPortID,
						}, nil
					}
					panic("test should not enter here")
				},
				DeleteFunc: func(ctx context.Context, id string) error {
					return nil
				},
			}

			client.LoadBalancerClientObj = &testing.CallbackLoadBalancerClient{
				DeleteFunc: func(ctx context.Context, id string, opts loadbalancers.DeleteOptsBuilder) error {
					if id == "888888-9999999-6666666" {
						octaviaDeleted = true
					}
					return nil
				},
			}

			client.PortClientObj = &testing.CallbackPortClient{
				ListFunc: func(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error) {
					return []ports.Port{}, nil
				},
				DeleteFunc: func(ctx context.Context, id string) error {
					return nil
				},
				GetFunc: func(ctx context.Context, id string) (*ports.Port, error) {
					if id == octaviaPortID {
						return &ports.Port{DeviceOwner: "Octavia", ID: id, DeviceID: "lb-888888-9999999-6666666"}, nil
					}

					return &ports.Port{
						ID:             yawolPortID,
						Name:           lb.Namespace + "/" + lb.Name,
						SecurityGroups: []string{"lallal"},
					}, nil
				},
				CreateFunc: func(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error) {
					loadBalancerReconciler.Log.Info("created.port")
					Expect(opts.(ports.CreateOpts).NetworkID).Should(Equal(lb.Spec.Infrastructure.NetworkID))
					Expect(opts.(ports.CreateOpts).Name).Should(Equal(lb.Namespace + "/" + lb.Name))

					return &ports.Port{
						ID:   yawolPortID,
						Name: lb.Namespace + "/" + lb.Name,
					}, nil
				},
				UpdateFunc: func(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error) {
					return &ports.Port{
						ID:   yawolPortID,
						Name: "lala",
					}, nil
				},
			}
		})

		It("should migrate the loadbalancer", func() {
			hopefully(lbNN, func(g Gomega, act LB) error {
				g.Expect(fipFound).To(BeTrue())
				g.Expect(fipUnbound).To(BeTrue())
				g.Expect(fipRebound).To(BeTrue())
				g.Expect(octaviaDeleted).To(BeTrue())

				g.Expect(act.Status.FloatingID).ToNot(BeNil())
				g.Expect(*act.Status.FloatingID).To(Equal("111111-2222222-33333"))

				var eventList v1.EventList
				err := k8sClient.List(ctx, &eventList, &runtimeClient.ListOptions{Namespace: lbNN.Namespace})
				if err != nil {
					return err
				}

				octaviaStartEvent := false
				octaviaEndEvent := false

				for _, event := range eventList.Items {
					if event.Reason == "OctaviaMigration" &&
						event.Message == "Found Octavia LB active. Reusing IP." &&
						event.InvolvedObject.Kind == "LoadBalancer" &&
						event.InvolvedObject.Name == lbNN.Name {
						octaviaStartEvent = true
					}
				}

				for _, event := range eventList.Items {
					if event.Reason == "OctaviaMigration" &&
						event.Message == "Deleted Octavia LoadBalancer." &&
						event.InvolvedObject.Kind == "LoadBalancer" &&
						event.InvolvedObject.Name == lbNN.Name {
						octaviaEndEvent = true
					}
				}

				g.Expect(octaviaStartEvent).To(BeTrue())
				g.Expect(octaviaEndEvent).To(BeTrue())

				return nil
			})

			By("cleaning up")
			loadBalancerReconciler.MigrateFromOctavia = false
			// create a new client so all os resources are already cleaned up
			client = testing.GetFakeClient()
		})
	}) // migrate from octavia context
}) // load balancer describe

func cleanupLB(lbNN types.NamespacedName, timeout time.Duration) {
	// delete LB
	Expect(k8sClient.Delete(ctx, &LB{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: lbNN.Namespace,
			Name:      lbNN.Name,
		},
	}),
	).Should(Succeed())

	// check if LB is deleted
	var actual LB
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, lbNN, &actual)).ToNot(Succeed())
	}, timeout, interval).Should(Succeed())
}

func getDesiredSecGroups(remoteID string) []rules.SecGroupRule {
	desiredSecGroups := []rules.SecGroupRule{}
	etherTypes := []rules.RuleEtherType{rules.EtherType4, rules.EtherType6}

	for _, etherType := range etherTypes {
		desiredSecGroups = append(
			desiredSecGroups,
			rules.SecGroupRule{
				EtherType: string(etherType),
				Direction: string(rules.DirEgress),
			},
			rules.SecGroupRule{
				EtherType:     string(etherType),
				Direction:     string(rules.DirIngress),
				Protocol:      string(rules.ProtocolVRRP),
				RemoteGroupID: remoteID,
			},
			rules.SecGroupRule{
				EtherType:    string(etherType),
				Direction:    string(rules.DirIngress),
				Protocol:     string(rules.ProtocolICMP),
				PortRangeMin: 0,
				PortRangeMax: 8,
			},
		)
	}

	return desiredSecGroups
}

func getMockLB(lbNN types.NamespacedName) *LB {
	return &LB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbNN.Name,
			Namespace: lbNN.Namespace,
		},
		Status: yawolv1beta1.LoadBalancerStatus{
			NodeRoleRef: &rbac.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     lbNN.Name,
			},
		},
		Spec: yawolv1beta1.LoadBalancerSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test-label": lbNN.Name,
				},
			},
			Replicas:                 1,
			ExternalIP:               nil,
			InternalLB:               false,
			Endpoints:                nil,
			Ports:                    nil,
			LoadBalancerSourceRanges: nil,
			Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{
				FloatingNetID: pointer.StringPtr("floatingnet-id"),
				NetworkID:     "network-id",
				Flavor: &yawolv1beta1.OpenstackFlavorRef{
					FlavorID: pointer.StringPtr("flavor-id"),
				},
				Image: &yawolv1beta1.OpenstackImageRef{
					ImageID: pointer.StringPtr("image-id"),
				},
				AuthSecretRef: v1.SecretReference{
					Name:      secretName,
					Namespace: lbNN.Namespace,
				},
			},
		},
	}
}

func updateLB(
	lbNN types.NamespacedName,
	update func(*LB),
) *LB {
	// handle "remote and local services are out of sync" error
	var actual LB
	tries := 0
	var err error = nil
	for {
		if tries += 1; tries > 100 {
			err = fmt.Errorf("retries exceeded")
			break
		}

		err = k8sClient.Get(ctx, lbNN, &actual)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		update(&actual)

		err = k8sClient.Update(ctx, &actual)
		if err == nil {
			break
		}

		if k8sErrors.IsNotFound(err) {
			continue
		}

		if !k8sErrors.IsConflict(err) {
			break
		}

		// resource is in conflict, so try again
		time.Sleep(100 * time.Millisecond)
	}

	Expect(err).Should(Succeed())
	return &actual
}

func hopefully(
	lbNN types.NamespacedName,
	check func(Gomega, LB) error,
) {
	Eventually(func(g Gomega) error {
		var lb LB
		err := k8sClient.Get(ctx, lbNN, &lb)
		if err != nil {
			return err
		}

		return check(g, lb)
	}, timeout, interval).Should(Succeed())
}

func patchStatus(lb *LB, status yawolv1beta1.LoadBalancerStatus) {
	jsonData, err := json.Marshal(status)
	Expect(err).To(BeNil())

	Expect(k8sClient.Status().Patch(
		context.Background(),
		lb,
		runtimeClient.RawPatch(types.MergePatchType, []byte(`{"status": `+string(jsonData)+`}`)),
	)).To(Succeed())
}
