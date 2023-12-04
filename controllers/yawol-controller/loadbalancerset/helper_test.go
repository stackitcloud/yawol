package loadbalancerset

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type relCondTest struct {
	conditions      *[]corev1.NodeCondition
	expiration      metav1.Time
	checkTransition bool
	expect          bool
	expectErr       error
}

var _ = FDescribeTable("areRelevantConditionsMet",
	func(t relCondTest) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: t.conditions,
			},
		}
		res, err := areRelevantConditionsMet(machine, t.expiration, t.checkTransition)
		if t.expectErr != nil {
			Expect(err).To(MatchError(err))
		}
		Expect(res).To(Equal(t.expect))
	},
	Entry("No Conditions", relCondTest{
		conditions: nil,
		expect:     false,
	}),
	Entry("empty Conditions", relCondTest{
		conditions: &[]corev1.NodeCondition{},
		expect:     false,
	}),
	Entry("all conditions met", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(helper.ConfigReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: false,
		expect:          true,
	}),
	Entry("a required condition is missing", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		expect: false,
	}),
	Entry("a unrelated condition is fale", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(helper.ConfigReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
			{Type: "foo", Status: corev1.ConditionFalse},
		},
		expect: true,
	}),
	Entry("a required condition is false", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{Type: corev1.NodeConditionType(helper.ConfigReady), Status: corev1.ConditionFalse},
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		expect:    false,
		expectErr: helper.ErrConditionsNotInCorrectState,
	}),
	Entry("a required condition is too old", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{
				Type:              corev1.NodeConditionType(helper.ConfigReady),
				Status:            corev1.ConditionTrue,
				LastHeartbeatTime: metav1.Time{Time: time.Time{}.Add(-5 * time.Minute)},
			},
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: false,
		expect:          false,
		expectErr:       helper.ErrConditionsLastHeartbeatTimeToOld,
	}),
	Entry("with transition check: a condition is failed, but just happened", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{
				Type:               corev1.NodeConditionType(helper.ConfigReady),
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Time{},
			},
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: true,
		expect:          true,
	}),
	Entry("with transition check: a condition is failed, some time ago", relCondTest{
		conditions: &[]corev1.NodeCondition{
			{
				Type:               corev1.NodeConditionType(helper.ConfigReady),
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Time{Time: time.Time{}.Add(-5 * time.Minute)},
			},
			{Type: corev1.NodeConditionType(helper.EnvoyReady), Status: corev1.ConditionTrue},
			{Type: corev1.NodeConditionType(helper.EnvoyUpToDate), Status: corev1.ConditionTrue},
		},
		checkTransition: true,
		expect:          false,
	}),
)

var _ = Describe("getDeletionCondition", func() {
	It("should return false if no conditions are set", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{}
		found, _ := getDeletionCondition(machine)
		Expect(found).To(BeFalse())
	})
	It("should return false if condition is not present", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{Type: "Something"},
				},
			},
		}
		found, _ := getDeletionCondition(machine)
		Expect(found).To(BeFalse())
	})
	It("should return false if condition is not present", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{Type: "Something"},
					{Type: deletionMarkerCondition, Reason: "a-reason"},
				},
			},
		}
		found, cond := getDeletionCondition(machine)
		Expect(found).To(BeTrue())
		Expect(cond.Reason).To(Equal("a-reason"))
	})
})

var _ = Describe("setDeletionCondition", func() {
	It("should work on an empty status", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{}
		setDeletionCondition(machine, corev1.ConditionTrue, "reason", "message")
		Expect(*machine.Status.Conditions).To(ConsistOf(And(
			HaveField("Type", deletionMarkerCondition),
			HaveField("Status", corev1.ConditionTrue),
			HaveField("Reason", "reason"),
			HaveField("Message", "message"),
			HaveField("LastHeartbeatTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
			HaveField("LastTransitionTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
	It("should work on an empty conditions slice", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{},
			},
		}
		setDeletionCondition(machine, corev1.ConditionTrue, "reason", "message")
		Expect(*machine.Status.Conditions).To(ConsistOf(
			HaveField("Type", deletionMarkerCondition),
		))
	})
	It("should work when the condition is not present yet", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{Type: "somehing"},
				},
			},
		}
		setDeletionCondition(machine, corev1.ConditionTrue, "reason", "message")
		Expect(*machine.Status.Conditions).To(ContainElement(And(
			HaveField("Type", deletionMarkerCondition),
			HaveField("LastHeartbeatTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
			HaveField("LastTransitionTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
	It("should not update the transition time, if the status didn't change", func() {
		transitionTime := metav1.Time{Time: time.Unix(0, 0)}
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{
						Type:               deletionMarkerCondition,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: transitionTime,
					},
				},
			},
		}
		setDeletionCondition(machine, corev1.ConditionTrue, "reason", "message")
		Expect(*machine.Status.Conditions).To(ContainElement(And(
			HaveField("Type", deletionMarkerCondition),
			HaveField("LastTransitionTime.Time", Equal(transitionTime.Time)),
			HaveField("LastHeartbeatTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
	It("should update the transition time if the status changes", func() {
		transitionTime := metav1.Time{Time: time.Unix(0, 0)}
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{
						Type:               deletionMarkerCondition,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: transitionTime,
					},
				},
			},
		}
		setDeletionCondition(machine, corev1.ConditionFalse, "reason", "message")
		Expect(*machine.Status.Conditions).To(ContainElement(And(
			HaveField("Type", deletionMarkerCondition),
			HaveField("LastTransitionTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
			HaveField("LastHeartbeatTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
})
