package loadbalancermachine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"
	openstackhelper "github.com/stackitcloud/yawol/internal/helper/openstack"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"
	os "github.com/stackitcloud/yawol/internal/openstack"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"

	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
)

const (
	// ServiceFinalizer Name of finalizer for controller4
	ServiceFinalizer             = "yawol.stackit.cloud/controller4"
	ServiceAccountNameAnnotation = "kubernetes.io/service-account.name"
)

var ipv4Regex = `^(((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.|$)){4})`
var ipv4RegexC, _ = regexp.Compile(ipv4Regex)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerMachineReconciler struct { //nolint:revive // naming from kubebuilder
	client.Client
	APIHost             string
	CACert              []byte
	Log                 logr.Logger
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	RecorderLB          record.EventRecorder
	APIEndpoint         string
	Metrics             *helpermetrics.LoadBalancerMachineMetricList
	getOsClientForIni   os.GetOSClientFunc
	WorkerCount         int
	OpenstackTimeout    time.Duration
	YawolletRequeueTime int
	DiscoveryClient     *discovery.DiscoveryClient
	RateLimiter         ratelimiter.RateLimiter
}

// Reconcile Reconciles a LoadBalancerMachine
func (r *LoadBalancerMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancermachine", req.NamespacedName)

	loadBalancerMachine := &yawolv1beta1.LoadBalancerMachine{}

	if err := r.Client.Get(ctx, req.NamespacedName, loadBalancerMachine); err != nil {
		// If not found just add an info log and ignore error
		if errors2.IsNotFound(err) {
			r.Log.Info("LoadBalancerMachine not found", "lbm", req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var err error
	var osClient os.Client

	osClient, err = openstackhelper.GetOpenStackClientForInfrastructure(
		ctx,
		r.Client,
		loadBalancerMachine.Spec.Infrastructure,
		r.getOsClientForIni,
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !loadBalancerMachine.GetDeletionTimestamp().IsZero() {
		// our finalizer is present, so lets handle any external dependency

		// delete openstack resources
		if err := r.deleteServer(ctx, osClient, loadBalancerMachine); err != nil {
			return ctrl.Result{}, err
		}
		requeue, err := r.deletePort(ctx, osClient, loadBalancerMachine)
		if err != nil {
			return ctrl.Result{}, err
		}
		if requeue {
			return ctrl.Result{RequeueAfter: helper.DefaultRequeueTime}, nil
		}

		// delete k8s resources
		if err := r.deleteSA(ctx, loadBalancerMachine); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.deleteRoleBinding(ctx, loadBalancerMachine); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.deleteRole(ctx, loadBalancerMachine); err != nil {
			return ctrl.Result{}, err
		}

		// remove metrics before finalizer to dont get old metrics
		helper.RemoveLoadBalancerMachineMetrics(loadBalancerMachine, r.Metrics)

		// remove our finalizer from the list and update it.
		r.Log.Info("Remove finalizer", "loadBalancerMachineName", loadBalancerMachine.Name)
		if err := kubernetes.RemoveFinalizerIfNeeded(ctx, r.Client, loadBalancerMachine, ServiceFinalizer); err != nil {
			return ctrl.Result{}, err
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	helper.ParseLoadBalancerMachineMetrics(loadBalancerMachine, r.Metrics)

	if err := kubernetes.AddFinalizerIfNeeded(ctx, r.Client, loadBalancerMachine, ServiceFinalizer); err != nil {
		return ctrl.Result{}, err
	}

	// get loadbalancer for debug settings
	loadbalancer := &yawolv1beta1.LoadBalancer{}
	if err := r.Client.Get(
		ctx,
		client.ObjectKey{
			Name:      loadBalancerMachine.Spec.LoadBalancerRef.Name,
			Namespace: loadBalancerMachine.Spec.LoadBalancerRef.Namespace,
		},
		loadbalancer,
	); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile ServiceAccount for yawollet access
	var sa v1.ServiceAccount
	var res ctrl.Result
	if sa, err = r.reconcileSA(ctx, loadBalancerMachine); err != nil {
		return res, err
	}

	// Reconcile ServiceAccountSecret for yawollet access
	if err := r.reconcileSecret(ctx, loadBalancerMachine, sa); err != nil {
		return res, err
	}

	// Reconcile Role for yawollet access
	if err := r.reconcileRole(ctx, loadBalancerMachine, loadbalancer); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile RoleBinding for yawollet access
	if err := r.reconcileRoleBinding(ctx, loadBalancerMachine); err != nil {
		return ctrl.Result{}, err
	}

	// check if reconcile is needed
	if !helper.LoadBalancerMachineOpenstackReconcileIsNeeded(loadBalancerMachine) {
		return ctrl.Result{RequeueAfter: helper.OpenstackReconcileTime}, nil
	}

	if err := r.reconcilePort(ctx, osClient, req, loadBalancerMachine, loadbalancer); err != nil {
		return ctrl.Result{}, err
	}

	var vip string
	vip, err = r.reconcilePortAddressPair(ctx, osClient, loadBalancerMachine, loadbalancer)
	if err != nil {
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileServer(ctx, osClient, loadbalancer, loadBalancerMachine, vip); err != nil {
		return ctrl.Result{}, err
	}

	if err := helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
		LastOpenstackReconcile: &metav1.Time{Time: time.Now()},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: helper.OpenstackReconcileTime}, nil
}

// SetupWithManager is used by kubebuilder to init the controller loop
func (r *LoadBalancerMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.getOsClientForIni == nil {
		r.getOsClientForIni = func(iniData []byte, overwrite os.OSClientOverwrite) (os.Client, error) {
			osClient := os.OSClient{}
			err := osClient.Configure(iniData, overwrite, r.OpenstackTimeout, r.Metrics.OpenstackMetrics)
			if err != nil {
				return nil, err
			}
			return &osClient, nil
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancerMachine{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
			RateLimiter:             r.RateLimiter,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *LoadBalancerMachineReconciler) reconcileSA(
	ctx context.Context,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
) (v1.ServiceAccount, error) {
	r.Log.Info("Check SA", "loadBalancerMachineName", loadBalancerMachine.Name)

	sa := v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancerMachine.Name,
			Namespace: loadBalancerMachine.Namespace,
		},
	}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(&sa), &sa)
	if err != nil {
		if errors2.IsNotFound(err) {
			if err = r.Client.Create(ctx, &sa); err != nil {
				return v1.ServiceAccount{}, fmt.Errorf("%w, error creating sa", err)
			}
			return sa, nil
		}
		return v1.ServiceAccount{}, err
	}

	saNamespacedName := types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}.String()
	if loadBalancerMachine.Status.ServiceAccountName == nil ||
		*loadBalancerMachine.Status.ServiceAccountName != saNamespacedName {
		if err := helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			ServiceAccountName: &saNamespacedName,
		}); err != nil {
			return sa, err
		}
	}

	return sa, nil
}

func (r *LoadBalancerMachineReconciler) reconcileSecret(
	ctx context.Context,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	sa v1.ServiceAccount,
) error {
	r.Log.Info("Check Secret", "loadBalancerMachineName", loadBalancerMachine.Name)
	if loadBalancerMachine.Status.ServiceAccountSecretName != nil {
		// no need to check if the secret is still present
		// on false secret, the machine will delete itself
		return nil
	}

	// use pre 1.24 created secret
	if len(sa.Secrets) > 0 {
		r.Log.Info("Use Secret from ServiceAccount", "loadBalancerMachineName", loadBalancerMachine.Name)
		namespacedName := types.NamespacedName{
			Name: sa.Secrets[0].Name,
			// the referenced secret has no namespace
			// since it is the same as the service account
			Namespace: sa.Namespace,
		}.String()

		// return since secret is already created
		return helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			ServiceAccountSecretName: &namespacedName,
		})
	}

	// check if kubernetes handles secret creation
	version, err := kubernetes.GetVersion(r.DiscoveryClient)
	if err != nil {
		return err
	}

	if version.IsLower(&kubernetes.Version{Major: 1, Minor: 24}) {
		// secret not created by kubernetes
		// requeue until created
		return helper.ErrSecretNotFound
	}

	r.Log.Info("Create Secret", "loadBalancerMachineName", loadBalancerMachine.Name)
	// secret does not exist, create a new one
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancerMachine.Name,
			Namespace: loadBalancerMachine.Namespace,
			Annotations: map[string]string{
				ServiceAccountNameAnnotation: sa.Name,
			},
		},
		Type: v1.SecretTypeServiceAccountToken,
	}

	err = r.Client.Get(ctx, client.ObjectKeyFromObject(&secret), &v1.Secret{})
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	if errors2.IsNotFound(err) {
		if err := r.Client.Create(ctx, &secret); err != nil {
			return err
		}
	}

	namespacedNameString := types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}.String()

	// patch secretName in status
	return helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
		ServiceAccountSecretName: &namespacedNameString,
	})
}

func (r *LoadBalancerMachineReconciler) reconcileRole(
	ctx context.Context,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	loadBalancer *yawolv1beta1.LoadBalancer,
) error {
	r.Log.Info("Check role", "loadBalancerMachineName", loadBalancerMachine.Name)

	rules := helper.GetRoleRules(loadBalancer, loadBalancerMachine)
	role := rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancerMachine.Name,
			Namespace: loadBalancerMachine.Namespace,
		},
		Rules: rules,
	}
	err := r.Client.Get(ctx, client.ObjectKey{Name: role.Name, Namespace: role.Namespace}, &role)
	if err != nil {
		if !errors2.IsNotFound(err) {
			return err
		}
		if err := r.Client.Create(ctx, &role); err != nil {
			return err
		}
		r.Log.Info("Role created", "loadBalancerMachineName", loadBalancerMachine.Name)
	}

	roleNamespacedName := types.NamespacedName{Name: role.Name, Namespace: role.Namespace}.String()
	if loadBalancerMachine.Status.RoleName == nil ||
		*loadBalancerMachine.Status.RoleName != roleNamespacedName {
		if err := helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			RoleName: &roleNamespacedName,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) reconcileRoleBinding(
	ctx context.Context,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
) error {
	r.Log.Info("Check rolebinding", "loadBalancerMachineName", loadBalancerMachine.Name)

	rb := rbac.RoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancerMachine.Name,
			Namespace: loadBalancerMachine.Namespace,
		},
		Subjects: []rbac.Subject{{
			Kind:      "ServiceAccount",
			Name:      loadBalancerMachine.Name,
			Namespace: loadBalancerMachine.Namespace,
		}},
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "Role",
			Name:     loadBalancerMachine.Name,
		},
	}

	err := r.Client.Get(ctx, client.ObjectKey{Name: rb.Name, Namespace: rb.Namespace}, &rb)
	if err != nil {
		if !errors2.IsNotFound(err) {
			return err
		}
		if err := r.Client.Create(ctx, &rb); err != nil {
			return err
		}
		r.Log.Info("rolebinding created", "loadBalancerMachineName", loadBalancerMachine.Name)
	}

	roleBindingNamespacedName := types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}.String()
	if loadBalancerMachine.Status.RoleName == nil ||
		*loadBalancerMachine.Status.RoleName != roleBindingNamespacedName {
		if err := helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			RoleBindingName: &roleBindingNamespacedName,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) reconcilePort( //nolint: gocyclo // TODO reduce complexity in future
	ctx context.Context,
	osClient os.Client,
	req ctrl.Request,
	lbm *yawolv1beta1.LoadBalancerMachine,
	lb *yawolv1beta1.LoadBalancer,
) error {
	var err error

	// TODO cleanup after removing deprecated fields
	if lbm.Status.PortID != nil && lbm.Status.DefaultPortID == nil { //nolint: staticcheck // needed to be backwards compatible
		if err := helper.PatchLBMStatus(
			ctx,
			r.Client.Status(),
			lbm,
			yawolv1beta1.LoadBalancerMachineStatus{DefaultPortID: lbm.Status.PortID}, //nolint: staticcheck // needed to be backwards compatible
		); err != nil {
			return err
		}
	}

	// TODO cleanup after removing deprecated fields
	if lbm.Status.PortID != nil && lbm.Status.DefaultPortID != nil && //nolint: staticcheck // needed to be backwards compatible
		*lbm.Status.PortID == *lbm.Status.DefaultPortID { //nolint: staticcheck // needed to be backwards compatible
		if err := helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "portID"); err != nil {
			return err
		}
	}

	// TODO cleanup after removing deprecated fields
	var networkID string
	if lbm.Spec.Infrastructure.NetworkID != "" { //nolint: staticcheck // needed to be backwards compatible
		networkID = lbm.Spec.Infrastructure.NetworkID //nolint: staticcheck // needed to be backwards compatible
	}
	if lbm.Spec.Infrastructure.DefaultNetwork.NetworkID != "" {
		networkID = lbm.Spec.Infrastructure.DefaultNetwork.NetworkID
	}

	if networkID == "" {
		return helper.ErrNoNetworkID
	}

	var subnetID string
	if lbm.Spec.Infrastructure.DefaultNetwork.SubnetID != nil {
		subnetID = *lbm.Spec.Infrastructure.DefaultNetwork.SubnetID
	}

	var portClient os.PortClient
	portClient, err = osClient.PortClient(ctx)
	if err != nil {
		return err
	}

	if lbm.Status.DefaultPortName == nil {
		portName := req.NamespacedName.String()
		if err := helper.PatchLBMStatus(
			ctx,
			r.Client.Status(),
			lbm,
			yawolv1beta1.LoadBalancerMachineStatus{DefaultPortName: &portName},
		); err != nil {
			return err
		}
	}

	if lbm.Status.DefaultPortName == nil {
		return helper.ErrPortNameEmpty
	}

	var port *ports.Port

	// find or create port
	if lbm.Status.DefaultPortID == nil {
		// try to find port by name
		port, err = openstackhelper.GetPortByName(ctx, portClient, *lbm.Status.DefaultPortName)
		if err != nil {
			return err
		}

		// create port
		if port == nil {
			port, err = openstackhelper.CreatePort(
				ctx,
				portClient,
				*lbm.Status.DefaultPortName,
				networkID,
				subnetID)
			if err != nil {
				r.Log.Info("unexpected error occurred claiming a port", "lbm", lbm.Name)
				return kubernetes.SendErrorAsEvent(r.RecorderLB, err, lbm)
			}
		}

		// double check so status won't be corrupted
		if port.ID == "" {
			r.Log.Info(helper.ErrPortIDEmpty.Error(), "lbm", lbm.Name)
			return helper.ErrPortIDEmpty
		}

		// TODO: LBM objects should not be dependant on lb objects
		if lb.Status.SecurityGroupID == nil {
			return helper.ErrSecGroupIDEmpty
		}

		_, err = openstackhelper.BindSecGroupToPortIfNeeded(ctx, portClient, lb.Status.SecurityGroupID, port)
		if err != nil {
			return err
		}

		r.Log.Info("successfully created port", "id", port.ID, "lbm", lbm.Name)

		if err := helper.PatchLBMStatus(
			ctx,
			r.Client.Status(),
			lbm,
			yawolv1beta1.LoadBalancerMachineStatus{DefaultPortID: &port.ID},
		); err != nil {
			return err
		}
	}

	// Check if port still exists properly
	if lbm.Status.DefaultPortID != nil {
		if port, err = openstackhelper.GetPortByID(ctx, portClient, *lbm.Status.DefaultPortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode:
				r.Log.Info("lbm port not found in openstack", "defaultPortID", *lbm.Status.DefaultPortID)
				// remove port from LB status so a new one gets created next reconcile
				if err := helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "defaultPortID"); err != nil {
					return err
				}
				return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
			default:
				r.Log.Info("unexpected error while fetching ports occurred")
				return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
			}
		}
	}

	if port == nil {
		return helper.ErrFailedToCreatePortForLBM
	}

	// Patch defaultPortIP to status
	if len(port.FixedIPs) >= 1 &&
		(lbm.Status.DefaultPortIP == nil || *lbm.Status.DefaultPortIP != port.FixedIPs[0].IPAddress) {
		if err := helper.PatchLBMStatus(ctx, r.Client.Status(), lbm, yawolv1beta1.LoadBalancerMachineStatus{
			DefaultPortIP: &port.FixedIPs[0].IPAddress,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) reconcilePortAddressPair(
	ctx context.Context,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
	lb *yawolv1beta1.LoadBalancer,
) (string, error) {
	var portClient os.PortClient

	var err error
	portClient, err = osClient.PortClient(ctx)
	if err != nil {
		return "", err
	}

	if lbm.Status.DefaultPortID == nil {
		r.Log.Info(helper.ErrLBMPortNotSet.Error(), "lbm", lbm.Name)
		return "", helper.ErrLBMPortNotSet
	}

	portLBM, err := openstackhelper.GetPortByID(ctx, portClient, *lbm.Status.DefaultPortID)
	if err != nil {
		return "", kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
	}

	portLB, err := openstackhelper.GetPortByID(ctx, portClient, lbm.Spec.PortID)
	if err != nil {
		return "", kubernetes.SendErrorAsEvent(r.Recorder, err, lb)
	}

	if portLB == nil || portLBM == nil {
		return "", helper.ErrLBOrLBMPortsAreNil
	}

	if len(portLB.FixedIPs) < 1 {
		return "", helper.ErrNoFixedIPForLBPort
	}

	var ipv4 string
	var addressPair []ports.AddressPair
	for _, ips := range portLB.FixedIPs {
		if ipv4RegexC.MatchString(ips.IPAddress) {
			ipv4 = ips.IPAddress
		}
		addressPair = append(addressPair, ports.AddressPair{
			IPAddress: ips.IPAddress,
			// MACAddress is set automatically by openstack
			// portLB.MACAddress would be the wrong MAC to set here
		})
	}

	if ipv4 == "" {
		return "", helper.ErrNoFixedIPForLBPort
	}

	if len(portLBM.AllowedAddressPairs) == len(portLB.FixedIPs) {
		uptodate := true
		for i := range portLBM.AllowedAddressPairs {
			if portLBM.AllowedAddressPairs[i].IPAddress != portLB.FixedIPs[i].IPAddress {
				uptodate = false
				break
			}
		}
		if uptodate {
			// port addressPairs are already uptodate, return only ipv4 until dualstack is implemented
			return ipv4, nil
		}
	}

	err = openstackhelper.SetAllowedAddressPairsInPort(ctx, portClient, portLBM, &addressPair)
	return ipv4, err
}

func (r *LoadBalancerMachineReconciler) reconcileServer(
	ctx context.Context,
	osClient os.Client,
	loadbalancer *yawolv1beta1.LoadBalancer,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	vip string,
) error {
	var srvClient os.ServerClient
	var err error

	srvClient, err = osClient.ServerClient(ctx)
	if err != nil {
		return err
	}

	// get kubeconfig which will be passed to VM user-data for yawollet access
	var kubeconfig string
	if kubeconfig, err = r.getKubeConfigForServiceAccount(ctx, loadBalancerMachine.Status.ServiceAccountSecretName); err != nil {
		return err
	}

	// Generate user-data for yawollet VM
	userData, err := helper.GenerateUserData(
		kubeconfig,
		loadbalancer,
		loadBalancerMachine,
		vip,
		r.YawolletRequeueTime,
	)
	if err != nil {
		return err
	}

	var srv *servers.Server

	srv, err = openstackhelper.GetServerByName(ctx, srvClient, loadBalancerMachine.Name)
	if err != nil {
		return err
	}

	// delete server if it is in error state
	if srv != nil && srv.Status == openstackhelper.ServerStatusError {
		return r.Client.Delete(ctx, loadBalancerMachine)
	}

	if loadBalancerMachine.Status.DefaultPortID == nil {
		return helper.ErrPortIDEmpty
	}

	if srv == nil {
		srv, err = r.createServer(
			ctx,
			srvClient,
			loadBalancerMachine,
			loadbalancer,
			userData,
		)
		if err != nil {
			return kubernetes.SendErrorAsEvent(r.RecorderLB, err, loadbalancer)
		}
	}

	// update serverID
	if loadBalancerMachine.Status.ServerID == nil || srv.ID != *loadBalancerMachine.Status.ServerID {
		if err := helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			ServerID: &srv.ID,
		}); err != nil {
			return err
		}
	}

	// update creationTimestamp
	if loadBalancerMachine.Status.CreationTimestamp == nil || srv.Created != loadBalancerMachine.Status.CreationTimestamp.Time {
		if err := helper.PatchLBMStatus(ctx, r.Client.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			CreationTimestamp: &metav1.Time{Time: srv.Created},
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) createServer(
	ctx context.Context,
	serverClient os.ServerClient,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	loadBalancer *yawolv1beta1.LoadBalancer,
	userdata string,
) (*servers.Server, error) {
	r.Log.Info("Create server", "loadBalancerMachineName", loadBalancerMachine.Name)

	var err error

	var imageID string
	imageID, err = helper.GetImageID(loadBalancerMachine.Spec.Infrastructure.Image)
	if err != nil {
		return nil, err
	}

	var flavorID string
	flavorID, err = helper.GetFlavorID(loadBalancerMachine.Spec.Infrastructure.Flavor)
	if err != nil {
		return nil, err
	}

	// TODO cleanup after removing deprecated fields
	var networkID string

	if loadBalancerMachine.Spec.Infrastructure.NetworkID != "" { //nolint: staticcheck // needed to be backwards compatible
		networkID = loadBalancerMachine.Spec.Infrastructure.NetworkID //nolint: staticcheck // needed to be backwards compatible
	}
	if loadBalancerMachine.Spec.Infrastructure.DefaultNetwork.NetworkID != "" {
		networkID = loadBalancerMachine.Spec.Infrastructure.DefaultNetwork.NetworkID
	}

	if networkID == "" {
		return nil, helper.ErrNoNetworkID
	}
	var serverNetworks []servers.Network
	serverNetworks = append(serverNetworks, servers.Network{UUID: networkID, Port: *loadBalancerMachine.Status.DefaultPortID})

	for i := range loadBalancerMachine.Spec.Infrastructure.AdditionalNetworks {
		// we don't test for overlapping networks because this can be a valid use case,
		// and it is hard to validate over multiple openstack objects.
		serverNetworks = append(serverNetworks, servers.Network{UUID: loadBalancerMachine.Spec.Infrastructure.AdditionalNetworks[i].NetworkID})
	}

	var createOpts servers.CreateOptsBuilder
	createOpts = &servers.CreateOpts{
		Name:             loadBalancerMachine.Name,
		FlavorRef:        flavorID,
		ImageRef:         imageID,
		SecurityGroups:   nil,
		UserData:         []byte(userdata),
		AvailabilityZone: loadBalancerMachine.Spec.Infrastructure.AvailabilityZone,
		Networks:         serverNetworks,
		Metadata:         nil,
	}

	var hintOpts servers.SchedulerHintOptsBuilder

	if loadBalancer.Spec.DebugSettings.Enabled {
		createOpts = &keypairs.CreateOptsExt{
			CreateOptsBuilder: createOpts,
			KeyName:           loadBalancer.Spec.DebugSettings.SshkeyName,
		}
	}

	if loadBalancerMachine.Spec.ServerGroupID != "" {
		hintOpts = &servers.SchedulerHintOpts{
			Group: loadBalancerMachine.Spec.ServerGroupID,
		}
	}

	var server *servers.Server
	server, err = openstackhelper.CreateServer(ctx, serverClient, createOpts, hintOpts)
	if err != nil {
		return nil, err
	}

	err = r.waitForServerStatus(ctx, serverClient, server.ID, []string{"BUILD"}, []string{"ACTIVE"}, 600)
	return server, err
}

func (r *LoadBalancerMachineReconciler) deleteServerAndWait(
	ctx context.Context,
	serverClient os.ServerClient,
	lbm *yawolv1beta1.LoadBalancerMachine,
	serverID string,
) error {
	if err := openstackhelper.DeleteServer(ctx, serverClient, serverID); err != nil {
		switch err.(type) {
		case gophercloud.ErrUnexpectedResponseCode:
			r.Log.Info("error deleting server, already deleted", "lbm", lbm.Name)
		default:
			r.Log.Info("an unexpected error occurred deleting server", "lbm", lbm.Name)
			return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
		}
	}

	return r.waitForServerStatus(
		ctx,
		serverClient,
		serverID,
		[]string{openstackhelper.ServerStatusActive, openstackhelper.ServerStatusStopped, openstackhelper.ServerStatusError},
		[]string{openstackhelper.ServerStatusDeleted},
		600,
	)
}

func (r *LoadBalancerMachineReconciler) deleteServer(
	ctx context.Context,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	var err error

	var serverClient os.ServerClient
	serverClient, err = osClient.ServerClient(ctx)
	if err != nil {
		return err
	}

	// delete server
	if lbm.Status.ServerID != nil {
		if err := r.deleteServerAndWait(ctx, serverClient, lbm, *lbm.Status.ServerID); err != nil {
			return err
		}
	}

	// clean orphan servers
	serverName := lbm.Name
	var serverList []servers.Server
	serverList, err = serverClient.List(ctx, servers.ListOpts{Name: serverName})
	if err != nil {
		return err
	}

	for i := range serverList {
		server := &serverList[i]
		// double check
		if server.ID == "" || server.Name != serverName {
			continue
		}

		if err := r.deleteServerAndWait(ctx, serverClient, lbm, server.ID); err != nil {
			return err
		}
	}

	return helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "serverID")
}

func (r *LoadBalancerMachineReconciler) deletePort(
	ctx context.Context,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
) (bool, error) {
	var err error
	var requeue bool
	var portClient os.PortClient
	portClient, err = osClient.PortClient(ctx)
	if err != nil {
		return false, err
	}

	if lbm.Status.DefaultPortID != nil {
		if err = openstackhelper.DeletePort(ctx, portClient, *lbm.Status.DefaultPortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode:
				_ = helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "defaultPortIP")
				if err := helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "defaultPortID"); err != nil {
					return false, err
				}
			default:
				r.Log.Info("an unexpected error occurred deleting port", "lbm", lbm.Name)
				return false, kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
			}
		}
		requeue = true
	}

	// TODO cleanup after removing deprecated fields
	if lbm.Status.PortID != nil { //nolint: staticcheck // needed to be backwards compatible
		//nolint: staticcheck // needed to be backwards compatible
		if err = openstackhelper.DeletePort(ctx, portClient, *lbm.Status.PortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode:
				if err := helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "portID"); err != nil {
					return false, err
				}
			default:
				r.Log.Info("an unexpected error occurred deleting port", "lbm", lbm.Name)
				return false, kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
			}
		}
		requeue = true
	}

	if requeue {
		return true, nil
	}

	if lbm.Status.DefaultPortName != nil {
		port, err := openstackhelper.GetPortByName(ctx, portClient, *lbm.Status.DefaultPortName)
		if err != nil {
			return false, err
		}
		if port != nil && port.ID != "" {
			if err = openstackhelper.DeletePort(ctx, portClient, port.ID); err != nil {
				switch err.(type) {
				case gophercloud.ErrUnexpectedResponseCode:
				default:
					r.Log.Info("an unexpected error occurred deleting port", "lbm", lbm.Name)
					return false, kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
				}
			}
			requeue = true
		} else {
			if err := helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "defaultPortName"); err != nil {
				return false, err
			}
		}
	}

	return requeue, nil
}

func (r *LoadBalancerMachineReconciler) deleteSA(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	sa := v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbm.Name,
			Namespace: lbm.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, &sa); client.IgnoreNotFound(err) != nil { //nolint: gocritic // ignore of not found is intended
		return err
	}

	return helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "serviceAccountName")
}

func (r *LoadBalancerMachineReconciler) deleteRoleBinding(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	rb := rbac.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbm.Name,
			Namespace: lbm.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, &rb); client.IgnoreNotFound(err) != nil { //nolint: gocritic // ignore of not found is intended
		return err
	}

	return helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "roleBindingName")
}

func (r *LoadBalancerMachineReconciler) deleteRole(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	role := rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbm.Name,
			Namespace: lbm.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, &role); client.IgnoreNotFound(err) != nil { //nolint: gocritic // ignore of not found is intended
		return err
	}

	return helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "roleName")
}

func (r *LoadBalancerMachineReconciler) waitForServerStatus(
	ctx context.Context,
	serverClient os.ServerClient,
	id string,
	pending []string,
	target []string,
	secs int,
) error {
	timeoutContext, cancel := context.WithTimeout(ctx, time.Duration(secs)*time.Second)
	defer cancel()

	return gophercloud.WaitFor(timeoutContext, func(context.Context) (bool, error) {
		current, err := openstackhelper.GetServerByID(ctx, serverClient, id)
		if err != nil {
			if _, ok := err.(gophercloud.ErrUnexpectedResponseCode); ok && slices.Contains(target, "DELETED") {
				return true, nil
			}
			return false, err
		}

		if slices.Contains(target, current.Status) {
			return true, nil
		}

		// if there is no pending statuses defined or current status is in the pending list, then continue polling
		if len(pending) == 0 || slices.Contains(pending, current.Status) {
			return false, nil
		}

		if current.Status == openstackhelper.ServerStatusError {
			return false, fmt.Errorf("%w: server status %+v",
				helper.ErrUnexpectedOpenstackStatus, current.Fault)
		}

		return false, fmt.Errorf("%w:  %q, wanted target %q",
			helper.ErrUnexpectedOpenstackStatus, current.Status, strings.Join(target, ", "))
	})
}

func (r *LoadBalancerMachineReconciler) getKubeConfigForServiceAccount(
	ctx context.Context,
	namespacedName *string,
) (string, error) {
	if namespacedName == nil {
		return "", fmt.Errorf("%w NamespacedName is nil", helper.ErrSecretNotFound)
	}

	sec := v1.Secret{}
	nn, err := kubernetes.ToNamespacedName(*namespacedName)
	if err != nil {
		return "", err
	}

	if err := r.Client.Get(ctx, nn, &sec); err != nil {
		return "", fmt.Errorf("%w could not get %s", helper.ErrSecretNotFound, nn.String())
	}

	var token, ca []byte
	var ok bool

	if token, ok = sec.Data["token"]; !ok {
		return "", fmt.Errorf("%w: %s", helper.ErrTokenNotFoundInSecret, sec.Name)
	}

	if ca, ok = sec.Data["ca.crt"]; !ok {
		return "", fmt.Errorf("%w: %s", helper.ErrCANotFoundInSecret, sec.Name)
	}

	config := api.Config{
		Kind:        "Config",
		APIVersion:  "v1",
		Preferences: api.Preferences{},
		Clusters: map[string]*api.Cluster{"default-cluster": {
			Server:                   r.APIEndpoint,
			CertificateAuthorityData: ca,
		}},
		AuthInfos: map[string]*api.AuthInfo{"user": {
			Token: string(token),
		}},
		Contexts: map[string]*api.Context{"context": {
			Cluster:   "default-cluster",
			AuthInfo:  "user",
			Namespace: nn.Namespace,
		}},
		CurrentContext: "context",
	}

	var kubeConfig []byte
	if kubeConfig, err = runtime.Encode(latest.Codec, &config); err != nil {
		return "", err
	}

	return string(kubeConfig), nil
}
