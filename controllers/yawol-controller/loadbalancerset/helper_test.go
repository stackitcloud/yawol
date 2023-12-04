package loadbalancerset

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
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

})
