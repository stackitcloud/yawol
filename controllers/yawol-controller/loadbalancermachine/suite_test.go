package loadbalancermachine

import (
	"context"
	"path/filepath"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"
	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg                           *rest.Config
	k8sClient                     client.Client
	testEnv                       *envtest.Environment
	loadBalancerMachineReconciler *LoadBalancerMachineReconciler
	ctx                           context.Context
	cancel                        context.CancelFunc
)

// globals
const (
	namespace  = "testns"
	secretName = "testsecret"
)

func TestController4(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(
		t,
		"Controller Suite",
	)
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "charts", "yawol-controller", "crds")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = yawolv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		Port:               9091,
		MetricsBindAddress: "0",
		Namespace:          namespace,
	})
	Expect(err).ToNot(HaveOccurred())

	ns := v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	Expect(k8sClient.Create(context.Background(), &ns)).Should(Succeed())

	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"cloudprovider.conf": []byte(``),
		},
	}
	Expect(k8sClient.Create(context.Background(), &secret)).Should(Succeed())

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())

	loadBalancerMachineReconciler = &LoadBalancerMachineReconciler{
		APIEndpoint:     "https://example.com",
		Client:          k8sManager.GetClient(),
		Log:             ctrl.Log.WithName("controllers").WithName("LoadBalancerMachine"),
		Scheme:          k8sManager.GetScheme(),
		Recorder:        k8sManager.GetEventRecorderFor("LoadBalancerMachine"),
		RecorderLB:      k8sManager.GetEventRecorderFor("yawol-service"),
		Metrics:         &helpermetrics.LoadBalancerMachineMetrics,
		DiscoveryClient: discoveryClient,
	}

	err = loadBalancerMachineReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
