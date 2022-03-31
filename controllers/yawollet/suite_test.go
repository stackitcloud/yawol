package controllers

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"testing"

	discoverygrpcv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	testv3 "github.com/envoyproxy/go-control-plane/pkg/test/v3"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	envoyCmd  *exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "charts", "yawol-controller", "crds")},
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
		Scheme:    scheme.Scheme,
		Namespace: "testns",
	})
	Expect(err).ToNot(HaveOccurred())

	// envoy grpc startup
	cache := cachev3.NewSnapshotCache(false, cachev3.IDHash{}, nil)

	// create init snapshot
	snapshot := cachev3.NewSnapshot(
		"1",
		[]types.Resource{},
		[]types.Resource{},
		[]types.Resource{},
		[]types.Resource{},
		[]types.Resource{},
		[]types.Resource{},
	)
	err = snapshot.Consistent()
	Expect(err).ToNot(HaveOccurred())

	err = cache.SetSnapshot("lb-id", snapshot)
	Expect(err).ToNot(HaveOccurred())

	// envoy server startup
	cb := &testv3.Callbacks{Debug: true}
	srv := serverv3.NewServer(ctx, cache, cb)

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(10000))
	grpcServer := grpc.NewServer(grpcOptions...)

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "127.0.0.1", 18000))
	Expect(err).ToNot(HaveOccurred())

	discoverygrpcv3.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)

	log.Printf("management server listening on %d\n", 18000)
	go func() {
		grpcServer.Serve(lis) // nolint: errcheck
	}()

	// start envoy
	envoyCmd = exec.Command("envoy", "-c", "../../image/envoy-config.yaml")
	err = envoyCmd.Start()
	Expect(err).ToNot(HaveOccurred())

	err = (&LoadBalancerReconciler{
		Client:                  k8sManager.GetClient(),
		Log:                     ctrl.Log.WithName("controllers").WithName("LoadBalancer"),
		Scheme:                  k8sManager.GetScheme(),
		Recorder:                k8sManager.GetEventRecorderFor("Loadbalancer"),
		LoadbalancerName:        "test-lb",
		LoadbalancerMachineName: "test-lbm",
		EnvoyCache:              cache,
		ListenAddress:           "127.0.0.1",
		RequeueTime:             1,
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
	err = envoyCmd.Process.Kill()
	Expect(err).ToNot(HaveOccurred())
})
