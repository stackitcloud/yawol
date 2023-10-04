package helper

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
)

var _ = Describe("loadbalancerClasses", Serial, Ordered, func() {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{},
	}

	It("should return correct loadbalancerClass from annotation", func() {
		s := svc.DeepCopy()
		s.Annotations = map[string]string{
			yawolv1beta1.ServiceClassName: "foo",
		}
		expected := "foo"
		Expect(getLoadBalancerClass(s)).To(Equal(expected))
	})

	It("should return correct loadbalancerClass from spec", func() {
		s := svc.DeepCopy()
		s.Spec.LoadBalancerClass = ptr.To("bar")
		expected := "bar"
		Expect(getLoadBalancerClass(s)).To(Equal(expected))
	})

	It("should return correct loadbalancerClass from annotation if also set in spec", func() {
		s := svc.DeepCopy()
		s.Annotations = map[string]string{
			yawolv1beta1.ServiceClassName: "foo",
		}
		s.Spec.LoadBalancerClass = ptr.To("bar")
		expected := "foo"
		Expect(getLoadBalancerClass(s)).To(Equal(expected))
	})

	It("should return empty string if no class is set", func() {
		s := svc.DeepCopy()
		expected := ""
		Expect(getLoadBalancerClass(s)).To(Equal(expected))
	})
})
