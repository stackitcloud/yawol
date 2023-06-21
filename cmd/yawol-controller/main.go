package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/stackitcloud/yawol/controllers/yawol-controller/loadbalancer"
	"github.com/stackitcloud/yawol/controllers/yawol-controller/loadbalancermachine"
	"github.com/stackitcloud/yawol/controllers/yawol-controller/loadbalancerset"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	discovery "k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.uber.org/zap/zapcore"
	"golang.org/x/time/rate"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	// Namespace in for LoadBalancer CRs
	EnvClusterNamespace = "CLUSTER_NAMESPACE"
	EnvAPIEndpoint      = "API_ENDPOINT"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yawolv1beta1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddrLb string
	var metricsAddrLbs string
	var metricsAddrLbm string

	var enableLeaderElection bool
	var probeAddr string

	var concurrentWorkersPerReconciler int
	var errorBackoffBaseDelay time.Duration
	var errorBackoffMaxDelay time.Duration
	var lbController bool
	var lbSetController bool
	var lbMachineController bool

	var yawolletRequeueTime int

	var openstackTimeout time.Duration

	// settings for leases
	var leasesDurationInt int
	var leasesRenewDeadlineInt int
	var leasesRetryPeriodInt int
	var leasesLeaderElectionResourceLock string
	var leasesDuration time.Duration
	var leasesRenewDeadline time.Duration
	var leasesRetryPeriod time.Duration

	flag.StringVar(&metricsAddrLb, "metrics-addr-lb", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&metricsAddrLbs, "metrics-addr-lbm", ":8081", "The address the metric endpoint binds to.")
	flag.StringVar(&metricsAddrLbm, "metrics-addr-lbs", ":8082", "The address the metric endpoint binds to.")

	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8083", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.IntVar(&concurrentWorkersPerReconciler, "concurrent-workers", 30, "Defines the amount of concurrent workers per reconciler.")
	flag.DurationVar(&errorBackoffBaseDelay, "error-backoff-base-delay", 5*time.Millisecond,
		"Defines the base delay of reconciles in case of an error.")
	flag.DurationVar(&errorBackoffMaxDelay, "error-backoff-max-delay", 1000*time.Second,
		"Defines the max delay of reconciles in case of an error.")
	flag.BoolVar(&lbController, "enable-loadbalancer-controller", false,
		"Enable loadbalancer controller manager. ")
	flag.BoolVar(&lbSetController, "enable-loadbalancerset-controller", false,
		"Enable loadbalancer-set controller manager. ")
	flag.BoolVar(&lbMachineController, "enable-loadbalancermachine-controller", false,
		"Enable loadbalancer-machine controller manager. ")

	flag.IntVar(&yawolletRequeueTime, "yawollet-requeue-time", 0,
		"yawollet requeue time in seconds for reconcile if object was successful reconciled. "+
			"Values less than 5 are set to 5 and greater than 170 are set to 170. "+
			"If unset the default from yawollet is used.")

	flag.DurationVar(&openstackTimeout, "openstack-timeout", 20*time.Second, "Timeout for all requests against Openstack.")

	flag.IntVar(&leasesDurationInt, "leases-duration", 60,
		"Is the time in seconds a non-leader will wait until forcing to acquire leadership.")
	flag.IntVar(&leasesRenewDeadlineInt, "leases-renew-deadline", 50,
		"Is the time in seconds how long the current controller will retry before giving up.")
	flag.IntVar(&leasesRetryPeriodInt, "leases-retry-period", 10,
		"Is the time in seconds how long the controller waits between lease actions.")
	flag.StringVar(&leasesLeaderElectionResourceLock, "leases-leader-election-resource-lock", "leases",
		"The resource type which is used for leader election (default 'leases', can be also: 'configmaps' or 'configmapsleases').")

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	leasesDuration = time.Duration(leasesDurationInt) * time.Second
	leasesRenewDeadline = time.Duration(leasesRenewDeadlineInt) * time.Second
	leasesRetryPeriod = time.Duration(leasesRetryPeriodInt) * time.Second

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if !lbController && !lbSetController && !lbMachineController {
		lbController, lbSetController, lbMachineController = true, true, true
	}

	// get cluster namespace or panic
	var clusterNamespace string
	if clusterNamespace = os.Getenv(EnvClusterNamespace); clusterNamespace == "" {
		panic("could not read env " + EnvClusterNamespace)
	}

	var err error
	var loadBalancerMgr manager.Manager
	var loadBalancerSetMgr manager.Manager
	var loadBalancerMachineMgr manager.Manager
	cfg := ctrl.GetConfigOrDie()

	rateLimiter := workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(errorBackoffBaseDelay, errorBackoffMaxDelay),
		// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)}, // default values
	)

	// LoadBalancer Controller
	if lbController {
		loadBalancerMgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                        scheme,
			MetricsBindAddress:            metricsAddrLb,
			Port:                          9443,
			LeaderElection:                enableLeaderElection,
			LeaderElectionReleaseOnCancel: true,
			LeaderElectionID:              "3a7ac996.stackit.cloud",
			LeaseDuration:                 &leasesDuration,
			RenewDeadline:                 &leasesRenewDeadline,
			RetryPeriod:                   &leasesRetryPeriod,
			LeaderElectionResourceLock:    leasesLeaderElectionResourceLock,
			Namespace:                     clusterNamespace,
		})
		if err != nil {
			setupLog.Error(err, "unable to start manager")
			os.Exit(1)
		}

		if err := (&loadbalancer.Reconciler{
			Client:           loadBalancerMgr.GetClient(),
			Log:              ctrl.Log.WithName("controller").WithName("LoadBalancer"),
			Scheme:           loadBalancerMgr.GetScheme(),
			WorkerCount:      concurrentWorkersPerReconciler,
			RecorderLB:       loadBalancerMgr.GetEventRecorderFor("yawol-service"),
			Recorder:         loadBalancerMgr.GetEventRecorderFor("LoadBalancer"),
			Metrics:          &helpermetrics.LoadBalancerMetrics,
			OpenstackTimeout: openstackTimeout,
			RateLimiter:      rateLimiter,
		}).SetupWithManager(loadBalancerMgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
			os.Exit(1)
		}
	}

	// LoadBalancerSet Controller
	if lbSetController {
		loadBalancerSetMgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                        scheme,
			MetricsBindAddress:            metricsAddrLbs,
			Port:                          9444,
			LeaderElection:                enableLeaderElection,
			LeaderElectionReleaseOnCancel: true,
			LeaderElectionID:              "rgp5vg43.stackit.cloud",
			LeaseDuration:                 &leasesDuration,
			RenewDeadline:                 &leasesRenewDeadline,
			RetryPeriod:                   &leasesRetryPeriod,
			LeaderElectionResourceLock:    leasesLeaderElectionResourceLock,
			Namespace:                     clusterNamespace,
		})
		if err != nil {
			setupLog.Error(err, "unable to start manager")
			os.Exit(1)
		}

		if err := (&loadbalancerset.LoadBalancerSetReconciler{
			Client:      loadBalancerSetMgr.GetClient(),
			Log:         ctrl.Log.WithName("controller").WithName("LoadBalancerSet"),
			Scheme:      loadBalancerSetMgr.GetScheme(),
			WorkerCount: concurrentWorkersPerReconciler,
			Recorder:    loadBalancerSetMgr.GetEventRecorderFor("LoadBalancerSet"),
			Metrics:     &helpermetrics.LoadBalancerSetMetrics,
			RateLimiter: rateLimiter,
		}).SetupWithManager(loadBalancerSetMgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LoadBalancerSet")
			os.Exit(1)
		}
		if err := (&loadbalancerset.LoadBalancerMachineStatusReconciler{
			Client:      loadBalancerSetMgr.GetClient(),
			Log:         ctrl.Log.WithName("controller").WithName("LoadBalancerMachineStatus"),
			Scheme:      loadBalancerSetMgr.GetScheme(),
			WorkerCount: concurrentWorkersPerReconciler,
			RateLimiter: rateLimiter,
		}).SetupWithManager(loadBalancerSetMgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LoadBalancerSet")
			os.Exit(1)
		}
	}

	// LoadBalancerMachine Controller
	if lbMachineController {
		var apiEndpoint string
		if apiEndpoint = os.Getenv(EnvAPIEndpoint); apiEndpoint == "" {
			panic("could not read env " + EnvAPIEndpoint)
		}

		discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(cfg)

		loadBalancerMachineMgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                        scheme,
			MetricsBindAddress:            metricsAddrLbm,
			Port:                          9445,
			LeaderElection:                enableLeaderElection,
			LeaderElectionReleaseOnCancel: true,
			LeaderElectionID:              "tanf7ges.stackit.cloud",
			LeaseDuration:                 &leasesDuration,
			RenewDeadline:                 &leasesRenewDeadline,
			RetryPeriod:                   &leasesRetryPeriod,
			LeaderElectionResourceLock:    leasesLeaderElectionResourceLock,
			Namespace:                     clusterNamespace,
		})
		if err != nil {
			setupLog.Error(err, "unable to start manager")
			os.Exit(1)
		}

		if err := (&loadbalancermachine.LoadBalancerMachineReconciler{
			Client:              loadBalancerMachineMgr.GetClient(),
			WorkerCount:         concurrentWorkersPerReconciler,
			APIHost:             loadBalancerMachineMgr.GetConfig().Host,
			CACert:              loadBalancerMachineMgr.GetConfig().CAData,
			Log:                 ctrl.Log.WithName("controller").WithName("LoadBalancerMachine"),
			Recorder:            loadBalancerMachineMgr.GetEventRecorderFor("LoadBalancerMachine"),
			RecorderLB:          loadBalancerMachineMgr.GetEventRecorderFor("yawol-service"),
			Scheme:              loadBalancerMachineMgr.GetScheme(),
			APIEndpoint:         apiEndpoint,
			Metrics:             &helpermetrics.LoadBalancerMachineMetrics,
			OpenstackTimeout:    openstackTimeout,
			YawolletRequeueTime: yawolletRequeueTime,
			DiscoveryClient:     discoveryClient,
			RateLimiter:         rateLimiter,
		}).SetupWithManager(loadBalancerMachineMgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LoadBalancerMachine")
			os.Exit(1)
		}
	}

	//+kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	signalHandler := ctrl.SetupSignalHandler()

	select {
	case err = <-startManager(signalHandler, loadBalancerMgr, lbController):
	case err = <-startManager(signalHandler, loadBalancerSetMgr, lbSetController):
	case err = <-startManager(signalHandler, loadBalancerMachineMgr, lbMachineController):
	}
	if err != nil {
		setupLog.Error(err, "received runtime error")
		os.Exit(1)
	}
}

func startManager(signalHandler context.Context, mgr ctrl.Manager, enabled bool) <-chan error {
	r := make(chan error)

	if !enabled {
		return r
	}

	go func() {
		defer close(r)
		if err := mgr.Start(signalHandler); err != nil {
			r <- err
		}
	}()

	return r
}
