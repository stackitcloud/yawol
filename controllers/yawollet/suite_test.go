package controllers

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	discoverygrpcv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	testv3 "github.com/envoyproxy/go-control-plane/pkg/test/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const (
	Namespace  = "testns"
	nameLB     = "test-lb"
	nameLBM    = "test-lbm"
	SnapshotID = "lb-id"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	envoyCmd  *exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
	aferoFs   afero.Fs
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	aferoFs = afero.NewMemMapFs()

	ctx, cancel = context.WithCancel(context.Background())

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
		Namespace: Namespace,
	})
	Expect(err).ToNot(HaveOccurred())

	// envoy grpc startup
	cache := cachev3.NewSnapshotCache(false, cachev3.IDHash{}, nil)

	// create init snapshot
	snapshot, err := cachev3.NewSnapshot("1", map[resource.Type][]types.Resource{
		resource.EndpointType: {},
		resource.ClusterType:  {},
		resource.RouteType:    {},
		resource.ListenerType: {},
		resource.RuntimeType:  {},
		resource.SecretType:   {},
	})
	Expect(err).ToNot(HaveOccurred())

	err = snapshot.Consistent()
	Expect(err).ToNot(HaveOccurred())

	err = cache.SetSnapshot(ctx, SnapshotID, snapshot)
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
		_ = grpcServer.Serve(lis)
	}()

	// start envoy
	envoyCmd = exec.Command("envoy", "-c", "../../image/envoy-config.yaml")
	Expect(envoyCmd.Start()).To(Succeed())

	err = (&LoadBalancerReconciler{
		Client:                  k8sManager.GetClient(),
		Log:                     ctrl.Log.WithName("controllers").WithName("LoadBalancer"),
		Scheme:                  k8sManager.GetScheme(),
		Recorder:                k8sManager.GetEventRecorderFor("Loadbalancer"),
		LoadbalancerName:        nameLB,
		LoadbalancerMachineName: nameLBM,
		EnvoyCache:              cache,
		ListenAddress:           "127.0.0.1",
		RequeueTime:             1,
		AferoFs:                 aferoFs,
	}).SetupWithManager(k8sManager)
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
	Expect(testEnv.Stop()).To(Succeed())
	Expect(envoyCmd.Process.Kill()).To(Succeed())
})
