package loadbalancerset

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// integration tests
var _ = Describe("LoadBalancerSet controller", Serial, Ordered, func() {
	const (
		LoadBalancerSetName      = "test-lbset"
		LoadBalancerSetNamespace = "test-lbset-namespace"

		LoadBalancerLabelKey   = "app"
		LoadBalancerLabelValue = "test_stub"

		LoadBalancerSetReplicas = 1

		FloatingNetID = "64e0c0ba-794d-4487-9383-c4c98cb469ed"
		NetworkID     = "a9d32d04-87f1-47ef-8da4-a8fda4353bc1"
		FlavorID      = "osAyv1W3z2TU5D6h"
		ImageID       = "64e0c0ba-794d-4487-9383-c4c98cb469ed"
		SecretName    = "cloud-provider-config"
		PortID        = "64e0c0ba-794d-4487-9383-c4c98cb469ed"

		timeout  = time.Second * 30
		interval = time.Millisecond * 250
	)

	setStub := yawolv1beta1.LoadBalancerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LoadBalancerSetName,
			Namespace: LoadBalancerSetNamespace,
			Labels:    map[string]string{LoadBalancerLabelKey: LoadBalancerLabelValue},
		},
		Spec: yawolv1beta1.LoadBalancerSetSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{LoadBalancerLabelKey: LoadBalancerLabelValue},
			},
			Replicas: LoadBalancerSetReplicas,
			Template: yawolv1beta1.LoadBalancerMachineTemplateSpec{
				Labels: map[string]string{LoadBalancerLabelKey: LoadBalancerLabelValue},
				Spec: yawolv1beta1.LoadBalancerMachineSpec{
					Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{
						DefaultNetwork: yawolv1beta1.LoadBalancerDefaultNetwork{
							FloatingNetID: ptr.To(FloatingNetID),
							NetworkID:     NetworkID,
						},
						Flavor: yawolv1beta1.OpenstackFlavorRef{
							FlavorID: ptr.To(FlavorID),
						},
						Image: yawolv1beta1.OpenstackImageRef{
							ImageID: ptr.To(ImageID),
						},
						AuthSecretRef: v1.SecretReference{
							Name:      SecretName,
							Namespace: LoadBalancerSetNamespace,
						},
					},
					PortID: PortID,
				},
			},
		},
	}

	key := client.ObjectKey{
		Name:      LoadBalancerSetName,
		Namespace: LoadBalancerSetNamespace,
	}

	// create
	Context("Valid LoadBalancerSet with one replica", func() {
		ctx := context.Background()
		It("create ns", func() {
			ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: LoadBalancerSetNamespace}, Spec: v1.NamespaceSpec{}}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
		})
		It("Should create successfully", func() {
			Expect(k8sClient.Create(ctx, &setStub)).Should(Succeed())
		})

		It("Should create a LoadBalancerMachine", func() {
			Eventually(func() int {
				return len(getChildMachines(ctx, &setStub))
			}, timeout, interval).Should(Equal(1))
		})

		It("Should be a valid LoadBalancerMachine", func() {
			machine := getChildMachines(ctx, &setStub)[0]
			Eventually(isValidMachine(&setStub, &machine)).Should(BeTrue())
		})
		It("Should eventually have a master", func() {
			By("Check current set condition")
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&setStub), &setStub)).To(Succeed())
			Expect(setStub.Status.Conditions).To(HaveLen(1))

			Expect(setStub.Status.Conditions).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Type":   Equal(helper.HasKeepalivedMaster),
				"Status": Equal(metav1.ConditionFalse),
			})))

			oldLastTransitionTime := meta.FindStatusCondition(setStub.Status.Conditions, helper.HasKeepalivedMaster).LastTransitionTime

			By("setting LBM as master")
			machine := getChildMachines(ctx, &setStub)[0]
			patch := client.MergeFrom(machine.DeepCopy())
			machine.Status.Conditions = &[]v1.NodeCondition{
				{
					Type:   v1.NodeConditionType(helper.KeepalivedMaster),
					Status: v1.ConditionTrue,
					Reason: "KeepalivedStatus",
				},
			}
			Expect(k8sClient.Status().Patch(ctx, &machine, patch)).To(Succeed())

			Eventually(func(g Gomega) {
				var set yawolv1beta1.LoadBalancerSet
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&setStub), &set)).To(Succeed())

				g.Expect(set.Status.Conditions).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"Type":   Equal(helper.HasKeepalivedMaster),
					"Status": Equal(metav1.ConditionTrue),
				})))
				newLastTransitionTime := meta.FindStatusCondition(set.Status.Conditions, helper.HasKeepalivedMaster).LastTransitionTime
				g.Expect(newLastTransitionTime).ToNot(Equal(oldLastTransitionTime))
			}, timeout, interval).Should(Succeed())
		})
	})

	// patch
	Context("Patch replicas to four on existing LoadBalancerSet", func() {
		ctx := context.Background()
		It("Should patch successfully", func() {
			patch := []byte(`{"spec": {"replicas": 4}}`)
			Expect(k8sClient.Patch(ctx, &setStub, client.RawPatch(types.MergePatchType, patch))).Should(Succeed())
		})

		It("Should create four LoadBalancerMachines", func() {
			Eventually(func() int {
				return len(getChildMachines(ctx, &setStub))
			}, timeout, interval).Should(Equal(4))
		})
	})

	// status & conditions
	Context("Set healthy machine conditions", func() {
		ctx := context.Background()
		cpy := make([]v1.NodeCondition, len(conditions))
		copy(cpy, conditions)
		It("Should be successfully", func() {
			childMachines := getChildMachines(ctx, &setStub)
			for _, machine := range childMachines {
				patch := client.MergeFrom(machine.DeepCopy())
				machine.Status.Conditions = &cpy
				err := k8sClient.Status().Patch(ctx, &machine, patch)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Should update loadBalancerSet status to four ready replicas", func() {
			Eventually(func() int {
				set := &yawolv1beta1.LoadBalancerSet{}
				err := k8sClient.Get(ctx, key, set)
				Expect(err).NotTo(HaveOccurred())
				return *set.Status.ReadyReplicas
			}, timeout, interval).Should(Equal(4))
		})
	})

	Context("Set unhealthy machine conditions to all machines", func() {
		ctx := context.Background()
		It("Should be successfully", func() {
			conditions[0] = v1.NodeCondition{
				Message:            "reconcile is not running",
				Reason:             "YawolletIsNotReady",
				Status:             "False",
				Type:               v1.NodeConditionType(helper.ConfigReady),
				LastHeartbeatTime:  metav1.Time{Time: time.Now()},
				LastTransitionTime: metav1.Time{Time: time.Now().Add(-6 * time.Minute)},
			}
			childMachines := getChildMachines(ctx, &setStub)

			for _, machine := range childMachines {
				patch := client.MergeFrom(machine.DeepCopy())
				machine.Status.Conditions = &conditions
				err := k8sClient.Status().Patch(ctx, &machine, patch)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Should update loadBalancerSet status to zero ready replicas", func() {
			Eventually(func() int {
				set := &yawolv1beta1.LoadBalancerSet{}
				err := k8sClient.Get(ctx, key, set)
				Expect(err).NotTo(HaveOccurred())
				return *set.Status.ReadyReplicas
			}, timeout, interval).Should(Equal(0))
		})
	})

	// delete
	Context("Delete LoadBalancerSet", func() {
		ctx := context.Background()
		It("Should be successfully", func() {
			Eventually(func() error {
				set := &yawolv1beta1.LoadBalancerSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      LoadBalancerSetName,
						Namespace: LoadBalancerSetNamespace,
					},
				}
				return k8sClient.Delete(ctx, set)
			}).Should(Succeed())
		})

		It("Should delete all LoadBalancerMachines", func() {
			Eventually(func() int {
				childMachines := &yawolv1beta1.LoadBalancerMachineList{}
				err := k8sClient.List(ctx, childMachines, &client.ListOptions{
					LabelSelector: labels.SelectorFromSet(map[string]string{LoadBalancerLabelKey: LoadBalancerLabelValue}),
					Namespace:     LoadBalancerSetNamespace,
				})
				Expect(err).NotTo(HaveOccurred())
				return len(childMachines.Items)
			}, timeout, interval).Should(Equal(0))
		})

		It("Should delete LoadBalancerSet", func() {
			Eventually(func() error {
				set := &yawolv1beta1.LoadBalancerSet{}
				return k8sClient.Get(ctx, key, set)
			}, timeout, interval).ShouldNot(Succeed())
		})
	})
})

func getChildMachines(ctx context.Context, set *yawolv1beta1.LoadBalancerSet) []yawolv1beta1.LoadBalancerMachine {
	var childMachines yawolv1beta1.LoadBalancerMachineList
	err := k8sClient.List(ctx, &childMachines, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(set.Spec.Selector.MatchLabels),
		Namespace:     set.Namespace,
	})
	if err != nil {
		return yawolv1beta1.LoadBalancerMachineList{}.Items
	}
	return childMachines.Items
}

func isValidMachine(set *yawolv1beta1.LoadBalancerSet, machine *yawolv1beta1.LoadBalancerMachine) bool {
	isNameValid := strings.HasPrefix(machine.Name, set.Name)
	isNamespaceValid := machine.Namespace == set.Namespace
	isLabelsValid := reflect.DeepEqual(machine.Labels, set.Spec.Selector.MatchLabels)
	isSpecValid := reflect.DeepEqual(machine.Spec, set.Spec.Template.Spec)
	return isNameValid && isNamespaceValid && isLabelsValid && isSpecValid
}

var (
	conditions = []v1.NodeCondition{
		{
			Status:             v1.ConditionStatus(helper.ConditionTrue),
			Type:               v1.NodeConditionType(helper.ConfigReady),
			LastHeartbeatTime:  metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Status:             v1.ConditionStatus(helper.ConditionTrue),
			Type:               v1.NodeConditionType(helper.EnvoyReady),
			LastHeartbeatTime:  metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Status:             v1.ConditionStatus(helper.ConditionTrue),
			Type:               v1.NodeConditionType(helper.EnvoyUpToDate),
			LastHeartbeatTime:  metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Status:             v1.ConditionStatus(helper.ConditionTrue),
			Type:               v1.NodeConditionType(helper.KeepalivedStatsFile),
			LastHeartbeatTime:  metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Status:             v1.ConditionStatus(helper.ConditionTrue),
			Type:               v1.NodeConditionType(helper.KeepalivedProcess),
			LastHeartbeatTime:  metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Status:             v1.ConditionStatus(helper.ConditionTrue),
			Type:               v1.NodeConditionType(helper.KeepalivedMaster),
			LastHeartbeatTime:  metav1.Time{Time: time.Now()},
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
	}
)

// unit tests
func TestIsMachineReady(t *testing.T) {
	cpy := make([]v1.NodeCondition, len(conditions))
	copy(cpy, conditions)
	machine := yawolv1beta1.LoadBalancerMachine{
		Status: yawolv1beta1.LoadBalancerMachineStatus{
			Conditions: &cpy,
		},
	}

	t.Run("All succeeded conditions should and fresh heartbeat result in ready machine", func(t *testing.T) {
		got := isMachineReady(machine)
		want := true

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}
	})

	t.Run("Failed condition should result in not ready machine", func(t *testing.T) {
		for index, condition := range *machine.Status.Conditions {
			cons := *machine.Status.Conditions
			if string(condition.Type) == string(helper.ConfigReady) {
				cons[index].Status = v1.ConditionStatus(helper.ConditionFalse)
				cons[index].LastTransitionTime = metav1.Time{Time: time.Now().Add(-10 * time.Second)}
			}
		}
		got := isMachineReady(machine)
		want := false

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}
	})

	t.Run("Heartbeat older than 180 seconds should result in not ready machine", func(t *testing.T) {
		for index, condition := range *machine.Status.Conditions {
			cons := *machine.Status.Conditions
			if string(condition.Type) == string(helper.ConfigReady) {
				cons[index].LastHeartbeatTime = metav1.Time{Time: time.Now().Add(-180 * time.Second)}
			}
		}
		got := isMachineReady(machine)
		want := false

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}
	})

	t.Run("Less than six conditions should result in not ready machine", func(t *testing.T) {
		for index, condition := range *machine.Status.Conditions {
			cons := *machine.Status.Conditions
			if string(condition.Type) == string(helper.ConfigReady) {
				cons[index] = v1.NodeCondition{}
			}
		}
		got := isMachineReady(machine)
		want := false

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}
	})
}

func TestShouldMachineBeDeleted(t *testing.T) {
	t.Run("Do not delete if creation within the last 5 minutes", func(t *testing.T) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: nil,
			},
		}
		got, _ := shouldMachineBeDeleted(machine)
		want := false

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}
	})

	t.Run("Do not delete if failed condition is not older than 5 minutes", func(t *testing.T) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				CreationTimestamp: &metav1.Time{Time: time.Now()},
				Conditions: &[]v1.NodeCondition{
					// on required condition is false, but not old
					{
						Status:             "False",
						Type:               v1.NodeConditionType(helper.ConfigReady),
						LastHeartbeatTime:  metav1.Time{Time: time.Now()},
						LastTransitionTime: metav1.Time{Time: time.Now()},
					},
					{
						Status:             "True",
						Type:               v1.NodeConditionType(helper.EnvoyReady),
						LastHeartbeatTime:  metav1.Time{Time: time.Now()},
						LastTransitionTime: metav1.Time{Time: time.Now()},
					},
					{
						Status:             "True",
						Type:               v1.NodeConditionType(helper.EnvoyUpToDate),
						LastHeartbeatTime:  metav1.Time{Time: time.Now()},
						LastTransitionTime: metav1.Time{Time: time.Now()},
					},
				},
			},
		}
		got, _ := shouldMachineBeDeleted(machine)
		want := false

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}
	})

	t.Run("Delete if creation is more than 10 minutes old and no conditions", func(t *testing.T) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Time{Time: time.Now().Add(-11 * time.Minute)}},
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: nil,
			},
		}
		got, gotErr := shouldMachineBeDeleted(machine)
		want := true
		wantErr := helper.ErrNotAllConditionsSet

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}

		if !errors.Is(gotErr, wantErr) {
			t.Errorf("Expected %v got %v", wantErr, gotErr)
		}
	})

	t.Run("Delete if heartbeat time is older than 5 minutes", func(t *testing.T) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				CreationTimestamp: &metav1.Time{Time: time.Now()},
				Conditions: &[]v1.NodeCondition{
					// one condition is stale, the others are up-to-date
					{
						Status:             v1.ConditionStatus(helper.ConditionTrue),
						Type:               v1.NodeConditionType(helper.ConfigReady),
						LastHeartbeatTime:  metav1.Time{Time: time.Now().Add(-6 * time.Minute)},
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-6 * time.Minute)},
					},
					{
						Status:             v1.ConditionStatus(helper.ConditionTrue),
						Type:               v1.NodeConditionType(helper.EnvoyReady),
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
					},
					{
						Status:             v1.ConditionStatus(helper.ConditionTrue),
						Type:               v1.NodeConditionType(helper.EnvoyUpToDate),
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		got, gotErr := shouldMachineBeDeleted(machine)
		want := true
		wantErr := helper.ErrConditionsLastHeartbeatTimeToOld

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}

		if !errors.Is(gotErr, wantErr) {
			t.Errorf("Expected %v got %v", wantErr, gotErr)
		}
	})

	t.Run("Delete if failed condition is older than 5 minutes", func(t *testing.T) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				CreationTimestamp: &metav1.Time{Time: time.Now()},
				Conditions: &[]v1.NodeCondition{
					// one condition failed for >5 Minutes
					{
						Status:             v1.ConditionStatus(helper.ConditionFalse),
						Type:               v1.NodeConditionType(helper.ConfigReady),
						LastHeartbeatTime:  metav1.Time{Time: time.Now()},
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-6 * time.Minute)},
					},
					{
						Status:             v1.ConditionStatus(helper.ConditionTrue),
						Type:               v1.NodeConditionType(helper.EnvoyReady),
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
					},
					{
						Status:             v1.ConditionStatus(helper.ConditionTrue),
						Type:               v1.NodeConditionType(helper.EnvoyUpToDate),
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
		got, gotErr := shouldMachineBeDeleted(machine)
		want := true
		wantErr := helper.ErrConditionsNotInCorrectState

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expected %v got %v", want, got)
		}

		if !errors.Is(gotErr, wantErr) {
			t.Errorf("Expected %v got %v", wantErr, gotErr)
		}
	})
}
