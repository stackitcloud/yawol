package loadbalancer

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/openstack"
	"github.com/stackitcloud/yawol/internal/openstack/testing"

	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("LB Status update", func() {
	one := 1
	var lbNN types.NamespacedName
	var lb yawolv1beta1.LoadBalancer
	var lbStatus yawolv1beta1.LoadBalancerStatus
	var lbRole rbac.Role
	var lbSet yawolv1beta1.LoadBalancerSet
	var lbSetStatus yawolv1beta1.LoadBalancerSetStatus

	BeforeEach(func() {
		lbNN = types.NamespacedName{
			Namespace: "testns",
			Name:      "testlb",
		}
		lbStatus = yawolv1beta1.LoadBalancerStatus{
			ReadyReplicas:     &one,
			Replicas:          &one,
			ExternalIP:        pointer.String("8.0.0.1"),
			FloatingID:        pointer.String("floating-id"),
			FloatingName:      pointer.String("floating-name"),
			PortID:            pointer.String("port-id"),
			PortName:          pointer.String("port-name"),
			SecurityGroupID:   pointer.String("sec-group-id"),
			SecurityGroupName: pointer.String("sec-group-name"),
		}
		lb = yawolv1beta1.LoadBalancer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lbNN.Name,
				Namespace: lbNN.Namespace,
				Annotations: map[string]string{
					"fifi": "fafa",
				},
			},
			Spec: yawolv1beta1.LoadBalancerSpec{
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"lala": "land",
					},
				},
				Replicas: 1,
				Options: yawolv1beta1.LoadBalancerOptions{
					InternalLB:               false,
					LoadBalancerSourceRanges: nil,
				},
				Endpoints: nil,
				Ports:     nil,
				Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{
					DefaultNetwork: yawolv1beta1.LoadBalancerDefaultNetwork{
						FloatingNetID: pointer.String("floatingnetid"),
						NetworkID:     "networkid",
					},
					Flavor: &yawolv1beta1.OpenstackFlavorRef{
						FlavorID: pointer.String("mycool-openstack-flavor-id"),
					},
					Image: &yawolv1beta1.OpenstackImageRef{
						ImageID: pointer.String("mycool-openstack-image-id"),
					},
					AuthSecretRef: v1.SecretReference{
						Name:      "testsecret",
						Namespace: "testns",
					},
				},
			},
			Status: lbStatus,
		}
		lbRole = rbac.Role{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: lbNN.Namespace,
				Name:      lbNN.Name,
			},
		}

		lbmSpec := yawolv1beta1.LoadBalancerMachineSpec{
			Infrastructure: lb.Spec.Infrastructure,
			PortID:         *lbStatus.PortID,
			LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
				Name:      lbNN.Name,
				Namespace: lbNN.Namespace,
			},
		}
		hash, err := helper.HashData(lbmSpec)
		Expect(err).Should(BeNil())
		lbSet = yawolv1beta1.LoadBalancerSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "testns",
				Name:      "testlb-" + hash,
				Labels: map[string]string{
					"lala": "land",
				},
			},
			Spec: yawolv1beta1.LoadBalancerSetSpec{
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"lala": "land",
					},
				},
				Replicas: 1,
				Template: yawolv1beta1.LoadBalancerMachineTemplateSpec{
					Labels: map[string]string{
						"lala": "land",
					},
					Spec: lbmSpec,
				},
			},
		}
		lbSetStatus = yawolv1beta1.LoadBalancerSetStatus{
			AvailableReplicas: intPtr(1),
			ReadyReplicas:     intPtr(1),
			Replicas:          intPtr(1),
		}
	})

	It("should create loadbalancerset", func() {
		By("Setup - Disable reconciliation loop")
		loadBalancerReconciler.skipReconciles = true

		By("Setup - Create Objects")
		Expect(k8sClient.Create(ctx, &lb)).Should(Succeed())
		patchStatus(&lb, lbStatus)
		Expect(k8sClient.Create(ctx, &lbRole))
		Expect(k8sClient.Create(ctx, &lbSet)).Should(Succeed())

		By("Setup - Mocks")

		loadBalancerReconciler.getOsClientForIni = func(iniData []byte) (openstack.Client, error) {
			return testing.GetFakeClient(), nil
		}

		By("Setup - Enable reconciliation loop")
		loadBalancerReconciler.skipReconciles = false

		By("Test - Patching status")
		Expect(patchLBSetStatus(&lbSet, lbSetStatus)).Should(Succeed())

		By("Validate - ExternalIP should be set")
		var actual yawolv1beta1.LoadBalancer
		Eventually(func() *string {
			err := k8sClient.Get(ctx, lbNN, &actual)
			if err != nil {
				return nil
			}
			return actual.Status.ExternalIP
		}).ShouldNot(BeNil())

		Eventually(func() *int {
			err := k8sClient.Get(ctx, lbNN, &actual)
			if err != nil {
				return nil
			}
			return actual.Status.Replicas
		}).Should(Equal(intPtr(1)))

		Eventually(func() *int {
			err := k8sClient.Get(ctx, lbNN, &actual)
			if err != nil {
				return nil
			}
			return actual.Status.ReadyReplicas
		}).Should(Equal(intPtr(1)))

		By("Teardown - LB Resource")
		Expect(k8sClient.Delete(ctx, &lb))
		Expect(k8sClient.Delete(ctx, &lbSet))
		Expect(k8sClient.Delete(ctx, &lbRole))
		Eventually(func() error {
			return k8sClient.Get(ctx, lbNN, &lb)
		}, timeout, interval).ShouldNot(Succeed())

		var lbSetList yawolv1beta1.LoadBalancerSetList
		Eventually(func() *int {
			err := k8sClient.List(ctx, &lbSetList)
			if err != nil {
				return nil
			}
			length := len(lbSetList.Items)
			return &length
		}, timeout, interval).Should(Equal(intPtr(0)))
	})
})

func patchLBSetStatus(lb *yawolv1beta1.LoadBalancerSet, status yawolv1beta1.LoadBalancerSetStatus) error {
	jsonData, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return k8sClient.Status().Patch(
		context.Background(),
		lb,
		client.RawPatch(types.MergePatchType, []byte(`{"status": `+string(jsonData)+`}`)),
	)
}

func intPtr(val int) *int {
	return &val
}
