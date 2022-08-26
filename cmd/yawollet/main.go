package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	controllers "github.com/stackitcloud/yawol/controllers/yawollet"
	"github.com/stackitcloud/yawol/internal/helper"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	testv3 "github.com/envoyproxy/go-control-plane/pkg/test/v3"
	"google.golang.org/grpc"

	discoverygrpcv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const grpcMaxStreams uint32 = 100

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yawolv1beta1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var namespace string
	var loadbalancerName string
	var loadbalancerMachineName string
	var listenAddress string
	var listenInterface string
	var requeueTime int

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. Default is disabled.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", "0", "The address the probe endpoint binds to. Default is disabled.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&namespace, "namespace", "", "The namespace from lb und lbm object.")
	flag.StringVar(&loadbalancerName, "loadbalancer-name", "", "Name of lb object.")
	flag.StringVar(&loadbalancerMachineName, "loadbalancer-machine-name", "", "Name of lbm object.")
	flag.StringVar(&listenAddress, "listen-address", "", "Address that envoy should listen.")
	flag.StringVar(&listenInterface, "listen-interface", "", "Interface that envoy should listen on. Ignored if listen-address is set.")
	flag.IntVar(&requeueTime, "requeue-time", 30, "Requeue Time for reconcile if object was successful reconciled. "+
		"Values less than 5 are set to 5 and greater than 50 are set to 50")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// ensure required flags
	if namespace == "" || loadbalancerName == "" || loadbalancerMachineName == "" {
		setupLog.Error(helper.ErrYawolletRequiredFlags, "unable to get all parameters")
		os.Exit(1)
	}

	// force requeue time between 5 and 50 (to be inside the heartbeat#time of yawol-controller)
	if requeueTime < 5 {
		requeueTime = 5
	} else if requeueTime > 50 {
		requeueTime = 50
	}

	// set listen address
	if listenAddress == "" {
		if listenInterface != "" {
			ifaces, _ := net.Interfaces()
			for _, i := range ifaces {
				if i.Name == listenInterface {
					addrs, _ := i.Addrs()
					for _, addr := range addrs {
						switch v := addr.(type) {
						case *net.IPNet:
							if v.IP.To4() != nil {
								listenAddress = v.IP.To4().String()
							}
						case *net.IPAddr:
							if v.IP.To4() != nil {
								listenAddress = v.IP.To4().String()
							}
						}
					}
				}
			}
			if listenAddress == "" {
				setupLog.Error(helper.ErrYawolletIPNotFound, "no IP found for "+listenInterface)
				os.Exit(1)
			}
		} else {
			listenAddress = "0.0.0.0"
		}
	}

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
	if err := snapshot.Consistent(); err != nil {
		setupLog.Error(err, "snapshot inconsistency: ", "snapshot", snapshot)
		os.Exit(1)
	}
	setupLog.Info("will serve snapshot", "snapshot", snapshot)

	if err := cache.SetSnapshot("lb-id", snapshot); err != nil {
		setupLog.Error(err, "snapshot error", "snapshot", snapshot)
		os.Exit(1)
	}

	// envoy server startup
	ctx := context.Background()
	cb := &testv3.Callbacks{Debug: true}
	srv := serverv3.NewServer(ctx, cache, cb)

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(grpcMaxStreams))
	grpcServer := grpc.NewServer(grpcOptions...) // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "127.0.0.1", 18000))
	if err != nil {
		log.Fatal(err)
	}

	discoverygrpcv3.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)

	log.Printf("management server listening on %d\n", 18000)
	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			setupLog.Error(err, "grpc server error")
		}
	}()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "9df1d9a0.stackit.cloud",
		Namespace:          namespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.LoadBalancerReconciler{
		Client:                  mgr.GetClient(),
		Log:                     ctrl.Log.WithName("controller").WithName("LoadBalancer"),
		Scheme:                  mgr.GetScheme(),
		LoadbalancerName:        loadbalancerName,
		LoadbalancerMachineName: loadbalancerMachineName,
		EnvoyCache:              cache,
		ListenAddress:           listenAddress,
		RequeueTime:             requeueTime,
		Recorder:                mgr.GetEventRecorderFor("yawollet"),
		RecorderLB:              mgr.GetEventRecorderFor("yawol-service"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
