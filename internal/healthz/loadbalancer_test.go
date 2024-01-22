package healthz_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlHealth "sigs.k8s.io/controller-runtime/pkg/healthz"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/healthz"
	"github.com/stackitcloud/yawol/internal/helper"
)

var _ = Describe("NewLoadBalancerRevisionHealthz", func() {
	var (
		k8sClient client.Client
		ctx       context.Context

		lb      *yawolv1beta1.LoadBalancer
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
		lbName := "super-lb"
		lbmName := "super-lbm"
		lb = &yawolv1beta1.LoadBalancer{ObjectMeta: metav1.ObjectMeta{
			Name:      lbName,
			Namespace: namespace,
		}}
		lbm = &yawolv1beta1.LoadBalancerMachine{ObjectMeta: metav1.ObjectMeta{
			Name:      lbmName,
			Namespace: namespace,
		}}

		checker = healthz.NewLoadBalancerRevisionHealthz(ctx, k8sClient, namespace, lbName, lbmName)
	})
	It("should error, when LoadBalancer don't exist", func() {
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())
		Expect(checker(nil)).To(MatchError(ContainSubstring("failed getting LoadBalancer")))
	})
	It("should error, when LoadBalancerMachine don't exist", func() {
		Expect(k8sClient.Create(ctx, lb)).To(Succeed())
		Expect(checker(nil)).To(MatchError(ContainSubstring("failed getting LoadBalancerMachine")))
	})
	It("should error, when the annotations don't match", func() {
		metav1.SetMetaDataAnnotation(&lb.ObjectMeta, helper.RevisionAnnotation, "foo")
		metav1.SetMetaDataAnnotation(&lbm.ObjectMeta, helper.RevisionAnnotation, "bar")
		Expect(k8sClient.Create(ctx, lb)).To(Succeed())
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())

		Expect(checker(nil)).To(MatchError(ContainSubstring("does not match")))
	})
	It("should succeed, when the annotations match", func() {
		metav1.SetMetaDataAnnotation(&lb.ObjectMeta, helper.RevisionAnnotation, "foo")
		metav1.SetMetaDataAnnotation(&lbm.ObjectMeta, helper.RevisionAnnotation, "foo")
		Expect(k8sClient.Create(ctx, lb)).To(Succeed())
		Expect(k8sClient.Create(ctx, lbm)).To(Succeed())

		Expect(checker(nil)).To(Succeed())
	})
})
