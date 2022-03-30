package target_controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg               *rest.Config
	k8sClient         client.Client
	testEnv           *envtest.Environment
	testInfraDefaults InfrastructureDefaults
	ctx               context.Context
	cancel            context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(
		t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}},
	)
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:        []string{filepath.Join("..", "..", "..", "charts", "yawol-controller", "crds")},
		ControlPlaneStartTimeout: time.Second * time.Duration(15),
	}

	args := testEnv.ControlPlane.GetAPIServer().Configure()
	args.Append("feature-gates", "IPv6DualStack=true")
	args.Append("service-cluster-ip-range", "10.244.0.0/16,fc00::0001:0000/112")

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
		MetricsBindAddress: "0",
	})
	Expect(err).ToNot(HaveOccurred())
	authSecretName := "secret"
	floatingNetworkId := "floatingNetID"
	networkId := "networkId"
	namespace := "default"
	flavorID := "flavorID"
	flavorRef := yawolv1beta1.OpenstackFlavorRef{FlavorID: &flavorID}
	imageID := "imageID"
	imageRef := yawolv1beta1.OpenstackImageRef{ImageID: &imageID}
	internalLB := false

	testInfraDefaults = InfrastructureDefaults{
		AuthSecretName:    &authSecretName,
		FloatingNetworkId: &floatingNetworkId,
		NetworkId:         &networkId,
		Namespace:         &namespace,
		FlavorRef:         &flavorRef,
		ImageRef:          &imageRef,
		InternalLB:        &internalLB,
	}

	err = (&ServiceReconciler{
		TargetClient:           k8sManager.GetClient(),
		ControlClient:          k8sManager.GetClient(),
		InfrastructureDefaults: testInfraDefaults,
		Log:                    ctrl.Log.WithName("controllers").WithName("Service"),
		Scheme:                 k8sManager.GetScheme(),
		Recorder:               k8sManager.GetEventRecorderFor("Loadbalancer"),
		ClassName:              "",
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&NodeReconciler{
		TargetClient:           k8sManager.GetClient(),
		ControlClient:          k8sManager.GetClient(),
		InfrastructureDefaults: testInfraDefaults,
		Log:                    ctrl.Log.WithName("controllers").WithName("Node"),
		Scheme:                 k8sManager.GetScheme(),
		Recorder:               k8sManager.GetEventRecorderFor("Loadbalancer"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())
	close(done)
}, 60)

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
