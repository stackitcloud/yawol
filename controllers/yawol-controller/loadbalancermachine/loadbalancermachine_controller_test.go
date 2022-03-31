package loadbalancermachine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud"

	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	"dev.azure.com/schwarzit/schwarzit.ske/yawol.git/internal/openstack"
	"dev.azure.com/schwarzit/schwarzit.ske/yawol.git/internal/openstack/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type LB = yawolv1beta1.LoadBalancer
type LBM = yawolv1beta1.LoadBalancerMachine

const (
	timeout  = time.Second * 30
	interval = time.Millisecond * 250

	loadBalancerName        = "lb"
	loadBalancerMachineName = "lbm-"
)

var (
	loadBalancerCounter        = 0
	loadBalancerMachineCounter = 0
)

func getLBName() string {
	loadBalancerCounter += 1
	return loadBalancerName + fmt.Sprint(loadBalancerCounter)
}

func getLBMName() string {
	loadBalancerMachineCounter += 1
	return loadBalancerMachineName + fmt.Sprint(loadBalancerMachineCounter)
}

var _ = Describe("load balancer machine", func() {
	var (
		lb     *LB
		lbm    *LBM
		client *testing.MockClient
	)

	// reset closure variables to a known state
	BeforeEach(func() {
		lb = getMockLB()
		lbm = getMockLBM(lb)

		client = testing.GetFakeClient()
		// create the port used for the lb fip
		client.PortClientObj.Create(ctx, ports.CreateOpts{
			Name:      "port-id",
			NetworkID: "network-id",
		})

		loadBalancerMachineReconciler.getOsClientForIni = func(iniData []byte) (openstack.Client, error) {
			return client, nil
		}
	})

	JustBeforeEach(func() {
		createSA(lbm)

		Expect(k8sClient.Create(ctx, lb)).To(Succeed())
		patchLBStatus(lb, lb.Status)

		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())
		patchLBMStatus(lbm, lbm.Status)
	})

	AfterEach(func() {
		cleanupLBM(lbm, timeout)
		cleanupLB(lb, timeout)
	})

	When("creating a load balancer machine", func() {
		It("should create k8s resources", func() {
			lbmNN := runtimeClient.ObjectKeyFromObject(lbm)

			By("checking that the rbac role gets created")
			Eventually(func(g Gomega) {
				var actualRole rbac.Role
				g.Expect(k8sClient.Get(ctx, lbmNN, &actualRole)).To(Succeed())

				// TODO somehow the arrays are not equal even though they are
				g.Expect(len(actualRole.Rules)).To(Equal(len(getPolicyRules(lb, lbm))))
			}, timeout, interval).Should(Succeed())

			By("checking sa secret")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lbmNN, &v1.ServiceAccount{})).To(Succeed())
			}, timeout, interval).Should(Succeed())

			By("checking rolebinding")
			Eventually(func(g Gomega) {
				var roleBinding rbac.RoleBinding
				g.Expect(k8sClient.Get(ctx, lbmNN, &roleBinding)).To(Succeed())

				g.Expect(roleBinding.RoleRef.Name).To(Equal(lbm.Name))
				g.Expect(roleBinding.Subjects).To(BeEquivalentTo([]rbac.Subject{{
					Kind:      "ServiceAccount",
					Name:      lbm.Name,
					Namespace: namespace,
				}}))
			}, timeout, interval).Should(Succeed())
		})

		It("should create openstack resources", func() {
			lbmNN := runtimeClient.ObjectKeyFromObject(lbm)

			Eventually(func(g Gomega) {
				var actual yawolv1beta1.LoadBalancerMachine
				g.Expect(k8sClient.Get(ctx, lbmNN, &actual)).To(Succeed())

				g.Expect(actual.Status.ServerID).ToNot(BeNil())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				var actual yawolv1beta1.LoadBalancerMachine
				g.Expect(k8sClient.Get(ctx, lbmNN, &actual)).To(Succeed())

				g.Expect(actual.Status.ServerID).ToNot(BeNil())
				g.Expect(actual.Status.CreationTimestamp).ToNot(BeNil())
			}, timeout, interval).Should(Succeed())
		})

		It("should delete lbm", func() {
			lbmNN := runtimeClient.ObjectKeyFromObject(lbm)

			By("waiting until lbm is created")
			Eventually(func(g Gomega) {
				srvs, err := client.ServerClientObj.List(ctx, servers.ListOpts{})
				g.Expect(err).To(Succeed())
				g.Expect(len(srvs)).To(Equal(1))
			}, timeout, interval).Should(Succeed())

			By("deleting lbm")
			Expect(k8sClient.Delete(ctx, &LBM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      lbmNN.Name,
					Namespace: lbmNN.Namespace,
				},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lbmNN, &LBM{})).ToNot(Succeed())
			}, timeout, interval).Should(Succeed())

			By("checking that k8s resources get deleted")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lbmNN, &rbac.Role{})).ToNot(Succeed())
				g.Expect(k8sClient.Get(ctx, lbmNN, &rbac.RoleBinding{})).ToNot(Succeed())
				g.Expect(k8sClient.Get(ctx, lbmNN, &v1.ServiceAccount{})).ToNot(Succeed())
			}, timeout, interval).Should(Succeed())
		})
	}) // creating lbm

	When("openstack is not working", func() {
		BeforeEach(func() {
			client.ServerClientObj = &testing.CallbackServerClient{
				CreateFunc: func(ctx context.Context, opts servers.CreateOptsBuilder) (*servers.Server, error) {
					return &servers.Server{}, gophercloud.ErrDefault403{
						ErrUnexpectedResponseCode: gophercloud.ErrUnexpectedResponseCode{
							BaseError:      gophercloud.BaseError{},
							URL:            "",
							Method:         "",
							Expected:       nil,
							Actual:         0,
							Body:           []byte("Quota exceeded"),
							ResponseHeader: nil,
						},
					}
				},
				ListFunc: func(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error) {
					return []servers.Server{{}}, nil
				},
				GetFunc: func(ctx context.Context, id string) (*servers.Server, error) {
					return &servers.Server{}, nil
				},
				DeleteFunc: func(ctx context.Context, id string) error {
					return nil
				},
			}
		})

		AfterEach(func() {
			// so the cleanup can run properly
			client = testing.GetFakeClient()
		})

		It("should throw error events", func() {
			Eventually(func(g Gomega) {
				var curEvents v1.EventList
				g.Expect(k8sClient.List(ctx, &curEvents)).To(Succeed())

				eventFound := false
				for _, curEvent := range curEvents.Items {
					if curEvent.Message == "Quota exceeded" {
						eventFound = true
					}
				}
				g.Expect(eventFound).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	}) // openstack not working

	Context("HA features", func() {
		It("should create openstack resources", func() {
			lbmNN := runtimeClient.ObjectKeyFromObject(lbm)
			lbNN := runtimeClient.ObjectKeyFromObject(lb)

			Eventually(func(g Gomega) {
				var actual yawolv1beta1.LoadBalancerMachine
				g.Expect(k8sClient.Get(ctx, lbmNN, &actual)).To(Succeed())

				g.Expect(actual.Status.PortID).ToNot(BeNil())
				port, err := client.PortClientObj.Get(ctx, *actual.Status.PortID)
				g.Expect(err).To(Succeed())
				g.Expect(len(port.AllowedAddressPairs)).To(Equal(1))

				var actualLB yawolv1beta1.LoadBalancer
				g.Expect(k8sClient.Get(ctx, lbNN, &actualLB)).To(Succeed())

				g.Expect(actualLB.Status.PortID).ToNot(BeNil())
				portLB, err := client.PortClientObj.Get(ctx, *actualLB.Status.PortID)
				g.Expect(err).To(Succeed())

				g.Expect(port.AllowedAddressPairs[0].IPAddress).To(Equal(portLB.FixedIPs[0].IPAddress))
			}, timeout, interval).Should(Succeed())
		})
	}) // ha features
})

func getPolicyRules(lb *LB, lbm *LBM) []rbac.PolicyRule {
	return []rbac.PolicyRule{{
		Verbs:     []string{"create"},
		APIGroups: []string{""},
		Resources: []string{"events"},
	}, {
		Verbs:     []string{"list", "watch"},
		APIGroups: []string{"yawol.stackit.cloud"},
		Resources: []string{"loadbalancers"},
	}, {
		Verbs:         []string{"get"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancers"},
		ResourceNames: []string{lb.Name},
	}, {
		Verbs:     []string{"list", "watch"},
		APIGroups: []string{"yawol.stackit.cloud"},
		Resources: []string{"loadbalancermachines"},
	}, {
		Verbs:         []string{"get", "update", "patch"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancermachines"},
		ResourceNames: []string{lbm.Name},
	}, {
		Verbs:         []string{"get", "update", "patch"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancermachines/status"},
		ResourceNames: []string{lbm.Name},
	}}
}

func getMockLB() *LB {
	name := getLBName()
	return &LB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: yawolv1beta1.LoadBalancerStatus{
			SecurityGroupID:   pointer.StringPtr("secgroup-id"),
			SecurityGroupName: pointer.StringPtr("secgroup-name"),
			PortID:            pointer.StringPtr("0"),
			NodeRoleRef: &rbac.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     name,
			},
		},
		Spec: yawolv1beta1.LoadBalancerSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test-label": name,
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
					Namespace: namespace,
				},
			},
		},
	}
}

func getMockLBM(lb *LB) *LBM {
	name := getLBMName()
	return &LBM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: yawolv1beta1.LoadBalancerMachineSpec{
			LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
				Name:      lb.Name,
				Namespace: lb.Namespace,
			},
			FloatingID: "floating-id",
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
					Namespace: namespace,
				},
			},
		},
	}
}

func cleanupLB(lb *LB, timeout time.Duration) {
	// delete LB
	k8sClient.Delete(ctx, &LB{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: lb.Namespace,
			Name:      lb.Name,
		},
	})

	// check if LB is deleted
	var actual LB
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, runtimeClient.ObjectKeyFromObject(lb), &actual)).ToNot(Succeed())
	}, timeout, interval).Should(Succeed())
}

func cleanupLBM(lbm *LBM, timeout time.Duration) {
	// delete LBM
	k8sClient.Delete(ctx, &LBM{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: lbm.Namespace,
			Name:      lbm.Name,
		},
	})

	// check if LBM is deleted
	var actual LBM
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, runtimeClient.ObjectKeyFromObject(lbm), &actual)).ToNot(Succeed())
	}, timeout, interval).Should(Succeed())

	// check if custom created SA is deleted
	var actualSA v1.ServiceAccount
	Eventually(func(g Gomega) error {
		err := k8sClient.Get(ctx, runtimeClient.ObjectKeyFromObject(lbm), &actualSA)
		if err != nil && runtimeClient.IgnoreNotFound(err) == nil {
			return nil
		}
		return fmt.Errorf("SA not deleted")
	}, timeout, interval).Should(Succeed())
}

func patchLBStatus(lb *LB, status yawolv1beta1.LoadBalancerStatus) {
	jsonData, err := json.Marshal(status)
	Expect(err).To(BeNil())

	Expect(k8sClient.Status().Patch(
		context.Background(),
		lb,
		runtimeClient.RawPatch(types.MergePatchType, []byte(`{"status": `+string(jsonData)+`}`)),
	)).To(Succeed())
}

func patchLBMStatus(lbm *LBM, status yawolv1beta1.LoadBalancerMachineStatus) {
	jsonData, err := json.Marshal(status)
	Expect(err).To(BeNil())

	Expect(k8sClient.Status().Patch(
		context.Background(),
		lbm,
		runtimeClient.RawPatch(types.MergePatchType, []byte(`{"status": `+string(jsonData)+`}`)),
	)).To(Succeed())
}

func createSA(lbm *LBM) {
	saSecret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbm.Name,
			Namespace: lbm.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": lbm.Name,
			},
		},
		Type: v1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			"token":  []byte(``),
			"ca.crt": []byte(``),
		},
	}
	Expect(k8sClient.Create(context.Background(), &saSecret)).Should(Succeed())

	Eventually(func(g Gomega) {
		sec := &v1.Secret{}
		g.Expect(k8sClient.Get(ctx, runtimeClient.ObjectKeyFromObject(lbm), sec)).To(Succeed())

		g.Expect(sec.Name).To(Equal(lbm.Name))
		g.Expect(sec.Namespace).To(Equal(lbm.Namespace))
		g.Expect(sec.UID).NotTo(BeNil())
	}, timeout, interval).Should(Succeed())

	{
		sa := &v1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lbm.Name,
				Namespace: lbm.Namespace,
			},
			Secrets: []v1.ObjectReference{{
				Kind:      "Secret",
				Name:      lbm.Name,
				Namespace: lbm.Namespace,
			}},
		}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())
	}

	Eventually(func(g Gomega) {
		sa := &v1.ServiceAccount{}
		g.Expect(k8sClient.Get(ctx, runtimeClient.ObjectKeyFromObject(lbm), sa)).To(Succeed())

		g.Expect(sa.Name).To(Equal(lbm.Name))
		g.Expect(sa.Namespace).To(Equal(lbm.Namespace))
		g.Expect(len(sa.Secrets)).To(Equal(1))
	}, timeout, interval).Should(Succeed())
}
