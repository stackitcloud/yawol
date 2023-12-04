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
	expectReason    string
}

var _ = DescribeTable("areRelevantConditionsMet",
	func(t relCondTest) {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: t.conditions,
			},
		}
		res, reason := areRelevantConditionsMet(machine, t.expiration, t.checkTransition)
		if t.expectReason != "" {
			Expect(reason).To(ContainSubstring(t.expectReason))
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
		expect:       false,
		expectReason: "required condition ConfigReady not present on machine",
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
		expect:       false,
		expectReason: "condition ConfigReady is in status False",
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
		expectReason:    "condition ConfigReady heartbeat is stale",
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
	It("should return nil if no conditions are set", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{}
		cond := findDeletionCondition(machine)
		Expect(cond).To(BeNil())
	})
	It("should return nil if condition is not present", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{Type: "Something"},
				},
			},
		}
		cond := findDeletionCondition(machine)
		Expect(cond).To(BeNil())
	})
	It("should return the correct condition", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{Type: "Something"},
					{Type: helper.DeletionMarkerCondition, Reason: "a-reason"},
				},
			},
		}
		cond := findDeletionCondition(machine)
		Expect(cond.Reason).To(Equal("a-reason"))
	})
})

var _ = Describe("setDeletionCondition", func() {
	It("should work on an empty status", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{}
		setDeletionCondition(machine, corev1.NodeCondition{Status: corev1.ConditionTrue, Reason: "Reason"})
		Expect(*machine.Status.Conditions).To(ConsistOf(And(
			HaveField("Type", helper.DeletionMarkerCondition),
			HaveField("Status", corev1.ConditionTrue),
			HaveField("Reason", "Reason"),
			HaveField("Message", ""),
			HaveField("LastTransitionTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
	It("should work on an empty conditions slice", func() {
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{},
			},
		}
		setDeletionCondition(machine, corev1.NodeCondition{Status: corev1.ConditionTrue})
		Expect(*machine.Status.Conditions).To(ConsistOf(
			HaveField("Type", helper.DeletionMarkerCondition),
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
		setDeletionCondition(machine, corev1.NodeCondition{Status: corev1.ConditionTrue})
		Expect(*machine.Status.Conditions).To(ContainElement(And(
			HaveField("Type", helper.DeletionMarkerCondition),
			HaveField("LastTransitionTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
	It("should not update the transition time, if the status didn't change", func() {
		transitionTime := metav1.Time{Time: time.Unix(0, 0)}
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{
						Type:               helper.DeletionMarkerCondition,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: transitionTime,
					},
				},
			},
		}
		setDeletionCondition(machine, corev1.NodeCondition{Status: corev1.ConditionTrue})
		Expect(*machine.Status.Conditions).To(ContainElement(And(
			HaveField("Type", helper.DeletionMarkerCondition),
			HaveField("LastTransitionTime.Time", Equal(transitionTime.Time)),
		)))
	})
	It("should update the transition time if the status changes", func() {
		transitionTime := metav1.Time{Time: time.Unix(0, 0)}
		machine := &yawolv1beta1.LoadBalancerMachine{
			Status: yawolv1beta1.LoadBalancerMachineStatus{
				Conditions: &[]corev1.NodeCondition{
					{
						Type:               helper.DeletionMarkerCondition,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: transitionTime,
					},
				},
			},
		}
		setDeletionCondition(machine, corev1.NodeCondition{Status: corev1.ConditionFalse})
		Expect(*machine.Status.Conditions).To(ContainElement(And(
			HaveField("Type", helper.DeletionMarkerCondition),
			HaveField("LastTransitionTime.Time", BeTemporally("~", time.Now(), 1*time.Second)),
		)))
	})
})
