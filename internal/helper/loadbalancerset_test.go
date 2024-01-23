package helper

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("LBSetHasKeepalivedMaster", func() {
	It("should return true if everything is ok", func() {
		set := &yawolv1beta1.LoadBalancerSet{}
		setKeepalivedCond(set, metav1.ConditionTrue, time.Now().Add(-3*time.Minute))

		ok, _ := LBSetHasKeepalivedMaster(set)
		Expect(ok).To(BeTrue())
	})

	It("should return false if the condition is true for < 2 Minutes", func() {
		set := &yawolv1beta1.LoadBalancerSet{}
		setKeepalivedCond(set, metav1.ConditionTrue, time.Now().Add(-1*time.Minute))

		ok, req := LBSetHasKeepalivedMaster(set)
		Expect(ok).To(BeFalse())
		Expect(req).To(BeNumerically("~", 1*time.Minute, 1*time.Second))
	})

	It("should return false if the condition is false for < 10 Minutes", func() {
		set := &yawolv1beta1.LoadBalancerSet{}
		setKeepalivedCond(set, metav1.ConditionFalse, time.Now().Add(-1*time.Minute))

		ok, req := LBSetHasKeepalivedMaster(set)
		Expect(ok).To(BeFalse())
		Expect(req).To(BeNumerically("~", 9*time.Minute, 1*time.Second))
	})
	It("should return true if the condition is false for > 10 Minutes", func() {
		set := &yawolv1beta1.LoadBalancerSet{}
		setKeepalivedCond(set, metav1.ConditionFalse, time.Now().Add(-11*time.Minute))

		ok, _ := LBSetHasKeepalivedMaster(set)
		Expect(ok).To(BeTrue())
	})

	It("should return false if the condition is absent for < 15 Minutes", func() {
		set := &yawolv1beta1.LoadBalancerSet{ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
		}}

		ok, req := LBSetHasKeepalivedMaster(set)
		Expect(ok).To(BeFalse())
		Expect(req).To(BeNumerically("~", 14*time.Minute, 1*time.Second))
	})
	It("should return true if the condition is absent for > 15 Minutes", func() {
		set := &yawolv1beta1.LoadBalancerSet{ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(time.Now().Add(-16 * time.Minute)),
		}}

		ok, _ := LBSetHasKeepalivedMaster(set)
		Expect(ok).To(BeTrue())
	})
})

func setKeepalivedCond(set *yawolv1beta1.LoadBalancerSet, status metav1.ConditionStatus, ltt time.Time) {
	set.Status.Conditions = []metav1.Condition{{
		Type:               HasKeepalivedMaster,
		Status:             status,
		LastTransitionTime: metav1.NewTime(ltt),
	}}
}
