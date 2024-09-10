package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	discoverygrpcv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	testv3 "github.com/envoyproxy/go-control-plane/pkg/test/v3"
	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	controllers "github.com/stackitcloud/yawol/controllers/yawollet"
	yawolhealthz "github.com/stackitcloud/yawol/internal/healthz"
	"github.com/stackitcloud/yawol/internal/helper"
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
	var namespace string
	var loadbalancerName string
	var loadbalancerMachineName string
	var listenAddress string
	var listenInterface string
	var requeueTime int
	var keepalivedStatsFile string

	fs := pflag.NewFlagSet("yawollet", pflag.ExitOnError)

	// register --kubeconfig flag in FlagSet
	configFlagSet := flag.NewFlagSet("config", flag.ContinueOnError)
	config.RegisterFlags(configFlagSet)
	fs.AddGoFlagSet(configFlagSet)

	fs.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. Default is disabled.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", "127.0.0.1:8080", "The address the probe endpoint binds to.")

	fs.StringVar(&namespace, "namespace", "", "The namespace from lb und lbm object.")
	fs.StringVar(&loadbalancerName, "loadbalancer-name", "", "Name of lb object.")
	fs.StringVar(&loadbalancerMachineName, "loadbalancer-machine-name", "", "Name of lbm object.")
	fs.StringVar(&listenAddress, "listen-address", "", "Address that envoy should listen.")
	fs.StringVar(&listenInterface, "listen-interface", "", "Interface that envoy should listen on. Ignored if listen-address is set.")
	fs.IntVar(&requeueTime, "requeue-time", 30, "Requeue Time in seconds for reconcile if object was successful reconciled. "+
		"Values less than 5 are set to 5 and greater than 170 are set to 170")

	fs.StringVar(&keepalivedStatsFile, "keepalived-stats-file", "/tmp/keepalived.stats",
		"Stats file for keepalived (default: /tmp/keepalived.stats). "+
			"If set to empty no keepalived stats will be used for conditions and metrics.")

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	zapFlagSet := flag.NewFlagSet("zap", flag.ContinueOnError)
	opts.BindFlags(zapFlagSet)
	fs.AddGoFlagSet(zapFlagSet)

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	ctx := context.Background()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// ensure required flags
	if namespace == "" || loadbalancerName == "" || loadbalancerMachineName == "" {
		setupLog.Error(helper.ErrYawolletRequiredFlags, "unable to get all parameters")
		os.Exit(1)
	}

	// force requeue time between 5 and 50 (to be inside the heartbeat#time of yawol-controller)
	if requeueTime < 5 {
		requeueTime = 5
	} else if requeueTime > 170 {
		requeueTime = 170
	}
	requeueDuration := time.Duration(requeueTime) * time.Second

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
	envoyCache := cachev3.NewSnapshotCache(false, cachev3.IDHash{}, nil)

	// create init snapshot
	snapshot, err := cachev3.NewSnapshot("1", map[resource.Type][]types.Resource{
		resource.EndpointType: {},
		resource.ClusterType:  {},
		resource.RouteType:    {},
		resource.ListenerType: {},
		resource.RuntimeType:  {},
		resource.SecretType:   {},
	})
	if err != nil {
		setupLog.Error(err, "error creating new snapshot")
		os.Exit(1)
	}

	if err := snapshot.Consistent(); err != nil {
		setupLog.Error(err, "snapshot inconsistency: ", "snapshot", snapshot)
		os.Exit(1)
	}
	setupLog.Info("will serve snapshot", "snapshot", snapshot)

	if err := envoyCache.SetSnapshot(ctx, "lb-id", snapshot); err != nil {
		setupLog.Error(err, "snapshot error", "snapshot", snapshot)
		os.Exit(1)
	}

	// envoy server startup
	cb := &testv3.Callbacks{Debug: true}
	srv := serverv3.NewServer(ctx, envoyCache, cb)

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
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace: {},
			},
			ByObject: map[client.Object]cache.ByObject{
				&yawolv1beta1.LoadBalancer{}:        {Field: fields.SelectorFromSet(fields.Set{"metadata.name": loadbalancerName})},
				&yawolv1beta1.LoadBalancerMachine{}: {Field: fields.SelectorFromSet(fields.Set{"metadata.name": loadbalancerMachineName})},
			},
		},
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
		EnvoyCache:              envoyCache,
		ListenAddress:           listenAddress,
		RequeueDuration:         requeueDuration,
		KeepalivedStatsFile:     keepalivedStatsFile,
		Recorder:                mgr.GetEventRecorderFor("yawollet"),
		RecorderLB:              mgr.GetEventRecorderFor("yawol-service"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	managerCtx := ctrl.SetupSignalHandler()

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// The readyz checks are used by keepalived for increasing the priority of machines that run a healthy yawollet for
	// the latest LoadBalancer revision (i.e., when the machine belongs to the current LoadBalancerSet).
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("informer-sync", yawolhealthz.NewCacheSyncHealthz(mgr.GetCache())); err != nil {
		setupLog.Error(err, "unable to set up informer ready check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("loadbalancer-heartbeat", yawolhealthz.NewHeartbeatHealthz(
		managerCtx, mgr.GetCache(), 2*requeueDuration, namespace, loadbalancerMachineName)); err != nil {
		setupLog.Error(err, "unable to set up LoadBalancer heartbeat ready check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("loadbalancer-revision", yawolhealthz.NewLoadBalancerRevisionHealthz(
		managerCtx, mgr.GetCache(), namespace, loadbalancerName, loadbalancerMachineName,
	)); err != nil {
		setupLog.Error(err, "unable to set up LoadBalancer revision ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(managerCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
