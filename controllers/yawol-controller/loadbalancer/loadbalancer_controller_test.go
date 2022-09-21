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
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/openstack"
	"github.com/stackitcloud/yawol/internal/openstack/testing"
	v1 "k8s.io/api/core/v1"

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

	When("internal lb is set", func() {
		BeforeEach(func() {
			lb.Spec.Ports = []v1.ServicePort{{
				Protocol: v1.ProtocolTCP,
				Port:     8080,
				NodePort: 30000,
			}}
			lb.Spec.Options.InternalLB = true
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
				act.Spec.Options.InternalLB = true
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
				g.Expect(lbset.Spec.Template.Spec.PortID).Should(Equal(*act.Status.PortID))
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
					FlavorID: pointer.String("somenewid"),
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
					{Protocol: v1.ProtocolTCP, Port: 8081},
					{Protocol: v1.ProtocolTCP, Port: 8082},
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
					{Protocol: v1.ProtocolTCP, Port: 8083},
					{Protocol: v1.ProtocolUDP, Port: 8084},
				}
				act.Spec.Options.LoadBalancerSourceRanges = []string{
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
					return fmt.Errorf("no additional sec group rules were applied %v/%v",
						len(rls), len(getDesiredSecGroups(*act.Status.SecurityGroupID))+6)
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
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, lb)).To(Succeed())
			updateLB(lbNN, func(l *LB) {
				l.Finalizers = []string{}
			})
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

	Context("clean up openstack", func() {
		When("there are additional ports", func() {
			count := 5
			BeforeEach(func() {
				c, _ := client.PortClient(ctx)
				for i := 0; i < count; i++ {
					_, err := c.Create(ctx, ports.CreateOpts{Name: lbNN.String()})
					Expect(err).To(Not(HaveOccurred()))
				}

				ports, err := c.List(ctx, ports.ListOpts{Name: lbNN.String()})
				Expect(err).To(Not(HaveOccurred()))
				Expect(len(ports)).To(Equal(count))
			})

			It("should delete the additional ports", func() {
				By("checking that portname is set")
				hopefully(lbNN, func(g Gomega, act LB) error {
					g.Expect(act.Status.PortName).To(Not(BeNil()))
					g.Expect(*act.Status.PortName == lbNN.String())
					return nil
				})

				By("checking if one of the ports got used")
				Eventually(func(g Gomega) {
					c, _ := client.PortClient(ctx)

					ports, err := c.List(ctx, ports.ListOpts{Name: lbNN.String()})
					g.Expect(err).To(Not(HaveOccurred()))
					g.Expect(len(ports)).To(Equal(count))
				}, timeout, interval).Should(Succeed())

				By("deleting the LB")
				cleanupLB(lbNN, timeout)

				By("checking that all ports are deleted")
				Eventually(func(g Gomega) {
					c, _ := client.PortClient(ctx)

					ports, err := c.List(ctx, ports.ListOpts{Name: lbNN.String()})
					g.Expect(err).To(Not(HaveOccurred()))
					g.Expect(len(ports)).To(Equal(0))
				}, timeout, interval).Should(Succeed())
			})
		})

		When("there are additional secgroups", func() {
			count := 5
			BeforeEach(func() {
				c, _ := client.GroupClient(ctx)
				for i := 0; i < count; i++ {
					_, err := c.Create(ctx, groups.CreateOpts{Name: lbNN.String()})
					Expect(err).To(Not(HaveOccurred()))
				}

				secGroups, err := c.List(ctx, groups.ListOpts{Name: lbNN.String()})
				Expect(err).To(Not(HaveOccurred()))
				Expect(len(secGroups)).To(Equal(count))
			})

			It("should delete the additional secgroups", func() {
				By("checking that secgroupname is set")
				hopefully(lbNN, func(g Gomega, act LB) error {
					g.Expect(act.Status.SecurityGroupName).To(Not(BeNil()))
					g.Expect(*act.Status.SecurityGroupName == lbNN.String())
					return nil
				})

				By("checking if one of the secgroups got used")
				Eventually(func(g Gomega) {
					c, _ := client.GroupClient(ctx)

					secGroups, err := c.List(ctx, groups.ListOpts{Name: lbNN.String()})
					g.Expect(err).To(Not(HaveOccurred()))
					g.Expect(len(secGroups)).To(Equal(count))
				}, timeout, interval).Should(Succeed())

				By("deleting the LB")
				cleanupLB(lbNN, timeout)

				By("checking that all secgroups are deleted")
				Eventually(func(g Gomega) {
					c, _ := client.GroupClient(ctx)

					secGroups, err := c.List(ctx, groups.ListOpts{Name: lbNN.String()})
					g.Expect(err).To(Not(HaveOccurred()))
					g.Expect(len(secGroups)).To(Equal(0))
				}, timeout, interval).Should(Succeed())
			})
		})
	}) // clean up openstack context

}) // load balancer describe

func cleanupLB(lbNN types.NamespacedName, timeout time.Duration) {
	// delete LB
	Expect(runtimeClient.IgnoreNotFound(k8sClient.Delete(ctx, &LB{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: lbNN.Namespace,
			Name:      lbNN.Name,
		},
	}),
	)).Should(Succeed())

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
				PortRangeMin: 1,
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
		Spec: yawolv1beta1.LoadBalancerSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test-label": lbNN.Name,
				},
			},
			Replicas: 1,
			Options: yawolv1beta1.LoadBalancerOptions{
				InternalLB:               false,
				LoadBalancerSourceRanges: nil,
			},
			Endpoints:          nil,
			Ports:              nil,
			ExistingFloatingIP: nil,
			Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{
				FloatingNetID: pointer.String("floatingnet-id"),
				NetworkID:     "network-id",
				Flavor: &yawolv1beta1.OpenstackFlavorRef{
					FlavorID: pointer.String("flavor-id"),
				},
				Image: &yawolv1beta1.OpenstackImageRef{
					ImageID: pointer.String("image-id"),
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
	var err error
	for {
		tries++
		if tries > 100 {
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
