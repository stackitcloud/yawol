package healthz_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/healthz"
	"github.com/stackitcloud/yawol/internal/helper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlHealth "sigs.k8s.io/controller-runtime/pkg/healthz"
)

var _ = Describe("NewHeartbeatHeathz", func() {
	var (
		k8sClient client.Client
		ctx       context.Context
		now       = time.Now()

		lbm     *yawolv1beta1.LoadBalancerMachine
		checker ctrlHealth.Checker
	)
	BeforeEach(func() {
		ctx = context.Background()
		scheme := runtime.NewScheme()
		Expect(yawolv1beta1.AddToScheme(scheme)).To(Succeed())
		k8sClient = fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		namespace := "default"
		lbmName := "super-lbm"
		lbm = &yawolv1beta1.LoadBalancerMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lbmName,
				Namespace: namespace,
			},
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{},
			},
		}

		checker = healthz.NewHeartbeatHeathz(ctx, k8sClient, 1*time.Minute, namespace, lbmName)
	})
	It("should error, when objects don't exist", func() {
		Expect(checker(nil)).To(MatchError(ContainSubstring("failed getting LoadBalancerMachine")))
	})
	It("should succeed when all conditions are true", func() {
		addLBMCondition(lbm, helper.ConfigReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyUpToDate, helper.ConditionTrue, now)
		// since we did not tell the fake about the status subresoure, create
		// also persists the status.
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())

		Expect(checker(nil)).To(Succeed())
	})
	It("should error when a condition is not true", func() {
		addLBMCondition(lbm, helper.ConfigReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyUpToDate, helper.ConditionFalse, now)
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())

		Expect(checker(nil)).To(MatchError(ContainSubstring("condition EnvoyUpToDate is in status False")))
	})
	It("should error, when not all contitions are there", func() {
		addLBMCondition(lbm, helper.ConfigReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyReady, helper.ConditionTrue, now)
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())

		Expect(checker(nil)).To(MatchError(ContainSubstring("required condition EnvoyUpToDate not present")))
	})
	It("should error, when a heartbeat is too old", func() {
		old := now.Add(-2 * time.Minute)
		addLBMCondition(lbm, helper.ConfigReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyReady, helper.ConditionTrue, now)
		addLBMCondition(lbm, helper.EnvoyUpToDate, helper.ConditionTrue, old)
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())

		Expect(checker(nil)).To(MatchError(ContainSubstring("condition EnvoyUpToDate heartbeat is stale")))
	})
})

func addLBMCondition(
	lbm *yawolv1beta1.LoadBalancerMachine,
	typ helper.LoadbalancerCondition,
	status helper.LoadbalancerConditionStatus,
	heartbeat time.Time,
) {
	*lbm.Status.Conditions = append(*lbm.Status.Conditions, corev1.NodeCondition{
		Type:              corev1.NodeConditionType(typ),
		Status:            corev1.ConditionStatus(status),
		LastHeartbeatTime: metav1.Time{Time: heartbeat},
		Reason:            "Reason",
		Message:           "message",
	})
}
