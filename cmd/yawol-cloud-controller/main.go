package main

import (
	"context"
	"flag"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"strconv"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/controlcontroller"
	"github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/targetcontroller"
	"github.com/stackitcloud/yawol/internal/helper"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

type loadbalancerClassNames []string

const (
	// Namespace in for LoadBalancer CRs
	EnvClusterNamespace = "CLUSTER_NAMESPACE"
	// Openstack Secret Name
	EnvAuthSecretName = "SECRET_NAME"
	// FIP ID in openstack
	EnvFloatingNetID = "FLOATING_NET_ID"
	// Openstack NetworkID for LB
	EnvNetworkID = "NETWORK_ID"
	// Flavor Information
	// one must be set
	EnvFlavorID     = "FLAVOR_ID"
	EnvFlavorName   = "FLAVOR_NAME"
	EnvFlavorSearch = "FLAVOR_SEARCH"
	// Image Information
	// one must be set
	EnvImageID     = "IMAGE_ID"
	EnvImageName   = "IMAGE_NAME"
	EnvImageSearch = "IMAGE_SEARCH"
	// Default Availability Zone must be set
	EnvAvailabilityZone = "AVAILABILITY_ZONE"
	// Set internal Flag to Loadbalancer CR true/false
	EnvInternalLB = "INTERNAL_LB"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yawolv1beta1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var controlEnableLeaderElection bool
	var targetEnableLeaderElection bool
	var targetKubeconfig string
	var controlKubeconfig string
	var classNames loadbalancerClassNames
	var emptyClassName bool
	// settings for leases
	var leasesDurationInt int
	var leasesRenewDeadlineInt int
	var leasesRetryPeriodInt int
	var leasesLeaderElectionResourceLock string
	var leasesDuration time.Duration
	var leasesRenewDeadline time.Duration
	var leasesRetryPeriod time.Duration

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&controlEnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&targetEnableLeaderElection, "target-leader-elect", false,
		"Enable leader election for target manager. "+
			"Enabling this will ensure there is only one active target manager.")
	flag.StringVar(&targetKubeconfig, "target-kubeconfig", "",
		"K8s credentials for watching the Service resources.")
	flag.StringVar(&controlKubeconfig, "control-kubeconfig", "",
		"K8s credentials for deploying the LoadBalancer resources.")
	flag.Var(&classNames, "classname",
		"Only listen to Services with the given className. Can be set multiple times. "+
			"If no classname is set it will defaults to "+helper.DefaultLoadbalancerClass+" "+
			"and services without class. See also --empty-classname.")
	flag.BoolVar(&emptyClassName, "empty-classname", true,
		"Listen to services without a loadBalancerClass. Default is true.")
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

	if len(classNames) == 0 {
		classNames = append(classNames, helper.DefaultLoadbalancerClass)
	}
	if emptyClassName {
		classNames = append(classNames, "")
	}

	leasesDuration = time.Duration(leasesDurationInt) * time.Second
	leasesRenewDeadline = time.Duration(leasesRenewDeadlineInt) * time.Second
	leasesRetryPeriod = time.Duration(leasesRetryPeriodInt) * time.Second

	infrastructureDefaults := getInfrastructureDefaultsFromEnvOrDie()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	targetMgr, err := ctrl.NewManager(getConfigFromKubeconfigOrDie(targetKubeconfig), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		LeaderElection:                targetEnableLeaderElection,
		LeaderElectionReleaseOnCancel: true,
		LeaderElectionID:              "4c878ae2.stackit.cloud",
		LeaseDuration:                 &leasesDuration,
		RenewDeadline:                 &leasesRenewDeadline,
		RetryPeriod:                   &leasesRetryPeriod,
		LeaderElectionResourceLock:    leasesLeaderElectionResourceLock,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	controlMgr, err := ctrl.NewManager(getConfigFromKubeconfigOrDie(controlKubeconfig), ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				*infrastructureDefaults.Namespace: {},
			},
		},
		LeaderElection:                controlEnableLeaderElection,
		LeaderElectionReleaseOnCancel: true,
		LeaderElectionID:              "4c878ae2.stackit.cloud",
		LeaseDuration:                 &leasesDuration,
		RenewDeadline:                 &leasesRenewDeadline,
		RetryPeriod:                   &leasesRetryPeriod,
		LeaderElectionResourceLock:    leasesLeaderElectionResourceLock,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	targetClient := targetMgr.GetClient()
	controlClient := controlMgr.GetClient()
	//+kubebuilder:scaffold:builder

	if err = targetMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err = targetMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err = (&targetcontroller.ServiceReconciler{
		TargetClient:           targetClient,
		ControlClient:          controlClient,
		InfrastructureDefaults: infrastructureDefaults,
		Log:                    ctrl.Log.WithName("controller").WithName("Service"),
		Scheme:                 targetMgr.GetScheme(),
		Recorder:               targetMgr.GetEventRecorderFor("yawol-cloud-controller"),
		ClassNames:             classNames,
	}).SetupWithManager(targetMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Service")
		os.Exit(1)
	}
	if err = (&targetcontroller.NodeReconciler{
		TargetClient:           targetClient,
		ControlClient:          controlClient,
		InfrastructureDefaults: infrastructureDefaults,
		Log:                    ctrl.Log.WithName("controller").WithName("Node"),
		Scheme:                 targetMgr.GetScheme(),
		Recorder:               targetMgr.GetEventRecorderFor("yawol-cloud-controller"),
	}).SetupWithManager(targetMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}

	if err = (&controlcontroller.LoadBalancerReconciler{
		TargetClient:  targetClient,
		ControlClient: controlClient,
		Log:           ctrl.Log.WithName("controller").WithName("LoadBalancer"),
		Scheme:        controlMgr.GetScheme(),
		Recorder:      targetMgr.GetEventRecorderFor("yawol-cloud-controller"),
	}).SetupWithManager(controlMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
		os.Exit(1)
	}

	if err = (&controlcontroller.EventReconciler{
		TargetClient:  targetClient,
		ControlClient: controlClient,
		Log:           ctrl.Log.WithName("controller").WithName("LoadBalancer"),
		Scheme:        controlMgr.GetScheme(),
		Recorder:      targetMgr.GetEventRecorderFor("yawol-cloud-controller"),
	}).SetupWithManager(controlMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	signalHandler := ctrl.SetupSignalHandler()

	select {
	case err = <-startManager(signalHandler, targetMgr):
	case err = <-startManager(signalHandler, controlMgr):
	}
	if err != nil {
		setupLog.Error(err, "received runtime error")
		os.Exit(1)
	}
}

func startManager(
	signalHandler context.Context,
	manager ctrl.Manager,
) <-chan error {
	r := make(chan error)

	go func() {
		defer close(r)
		if err := manager.Start(signalHandler); err != nil {
			r <- err
		}
	}()

	return r
}

func getConfigFromKubeconfigOrDie(kubeconfig string) *rest.Config {
	if kubeconfig == "inClusterConfig" {
		return ctrl.GetConfigOrDie()
	}

	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{}).ClientConfig()

	if err != nil {
		setupLog.Error(err, "could not load kubeconfig", "path", kubeconfig)
		os.Exit(1)
	}

	return restConfig
}

func getInfrastructureDefaultsFromEnvOrDie() targetcontroller.InfrastructureDefaults {
	var authSecretName string
	if authSecretName = os.Getenv(EnvAuthSecretName); authSecretName == "" {
		panic("could not read env " + EnvAuthSecretName)
	}

	var floatingNetworkID string
	if floatingNetworkID = os.Getenv(EnvFloatingNetID); floatingNetworkID == "" {
		panic("could not read env " + EnvFloatingNetID)
	}

	var networkID string
	if networkID = os.Getenv(EnvNetworkID); networkID == "" {
		panic("could not read env " + EnvNetworkID)
	}

	var clusterNamespace string
	if clusterNamespace = os.Getenv(EnvClusterNamespace); clusterNamespace == "" {
		panic("could not read env " + EnvClusterNamespace)
	}

	var flavorID *string
	if flavorID = pointer.String(os.Getenv(EnvFlavorID)); *flavorID == "" {
		flavorID = nil
	}
	var flavorName *string
	if flavorName = pointer.String(os.Getenv(EnvFlavorName)); *flavorName == "" {
		flavorName = nil
	}
	var flavorSearch *string
	if flavorSearch = pointer.String(os.Getenv(EnvFlavorSearch)); *flavorSearch == "" {
		flavorSearch = nil
	}
	if flavorID == nil && flavorName == nil && flavorSearch == nil {
		panic("could not read one of envs [" + EnvFlavorID + "," + EnvFlavorName + "," + EnvFlavorSearch + "]")
	}

	var imageID *string
	if imageID = pointer.String(os.Getenv(EnvImageID)); *imageID == "" {
		imageID = nil
	}
	var imageName *string
	if imageName = pointer.String(os.Getenv(EnvImageName)); *imageName == "" {
		imageName = nil
	}
	var imageSearch *string
	if imageSearch = pointer.String(os.Getenv(EnvImageSearch)); *imageSearch == "" {
		imageSearch = nil
	}
	if imageID == nil && imageName == nil && imageSearch == nil {
		panic("could not read one of envs [" + EnvImageID + "," + EnvImageName + "," + EnvImageSearch + "]")
	}

	// availability zone is optional, default is empty string
	availabilityZone := os.Getenv(EnvAvailabilityZone)

	var internalLb bool
	iLb := os.Getenv(EnvInternalLB)
	if iLb == "" {
		internalLb = false
	} else {
		var err error
		internalLb, err = strconv.ParseBool(iLb)
		if err != nil {
			panic(EnvInternalLB + " must match one of the following values: " +
				"'1', 't', 'T', 'true', 'TRUE', 'True', '0', 'f', 'F', 'false', 'FALSE', 'False'")
		}
	}

	return targetcontroller.InfrastructureDefaults{
		AuthSecretName:    pointer.String(authSecretName),
		FloatingNetworkID: pointer.String(floatingNetworkID),
		NetworkID:         pointer.String(networkID),
		Namespace:         pointer.String(clusterNamespace),
		FlavorRef: &yawolv1beta1.OpenstackFlavorRef{
			FlavorID:     flavorID,
			FlavorName:   flavorName,
			FlavorSearch: flavorSearch,
		},
		ImageRef: &yawolv1beta1.OpenstackImageRef{
			ImageID:     imageID,
			ImageName:   imageName,
			ImageSearch: imageSearch,
		},
		AvailabilityZone: pointer.String(availabilityZone),
		InternalLB:       pointer.Bool(internalLb),
	}
}

func (i *loadbalancerClassNames) String() string {
	return strings.Join(*i, ",")
}

func (i *loadbalancerClassNames) Set(value string) error {
	*i = append(*i, value)
	return nil
}
