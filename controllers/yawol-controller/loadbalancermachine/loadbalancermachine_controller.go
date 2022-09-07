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
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"

	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	// ServiceFinalizer Name of finalizer for controller4
	ServiceFinalizer   = "yawol.stackit.cloud/controller4"
	DefaultRequeueTime = 10 * time.Millisecond
)

var ipv4Regex = `^(((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.|$)){4})`
var ipv4RegexC, _ = regexp.Compile(ipv4Regex)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerMachineReconciler struct { //nolint:revive // naming from kubebuilder
	client.Client
	APIHost           string
	CACert            []byte
	Log               logr.Logger
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	RecorderLB        record.EventRecorder
	APIEndpoint       string
	Metrics           *helpermetrics.LoadBalancerMachineMetricList
	getOsClientForIni func(iniData []byte) (os.Client, error)
	WorkerCount       int
	OpenstackTimeout  time.Duration
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

	helper.ParseLoadBalancerMachineMetrics(loadBalancerMachine, r.Metrics)

	var err error
	var osClient os.Client

	osClient, err = openstackhelper.GetOpenStackClientForAuthRef(
		ctx,
		r.Client,
		loadBalancerMachine.Spec.Infrastructure.AuthSecretRef,
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
		if err := r.deletePort(ctx, osClient, loadBalancerMachine); err != nil {
			return ctrl.Result{}, err
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

		// remove our finalizer from the list and update it.
		r.Log.Info("Remove finalizer", "loadBalancerMachineName", loadBalancerMachine.Name)

		if err := kubernetes.RemoveFinalizerIfNeeded(ctx, r.Client, loadBalancerMachine, ServiceFinalizer); err != nil {
			return ctrl.Result{}, err
		}

		helper.RemoveLoadBalancerMachineMetrics(loadBalancerMachine, r.Metrics)

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

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
		return ctrl.Result{}, nil
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

	if err := r.reconcileServer(ctx, osClient, loadbalancer, loadBalancerMachine, sa, vip); err != nil {
		return ctrl.Result{}, err
	}

	if err := helper.PatchLBMStatus(ctx, r.Status(), loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
		LastOpenstackReconcile: &metav1.Time{Time: time.Now()},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager is used by kubebuilder to init the controller loop
func (r *LoadBalancerMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.getOsClientForIni == nil {
		r.getOsClientForIni = func(iniData []byte) (os.Client, error) {
			osClient := os.OSClient{}
			err := osClient.Configure(iniData, r.OpenstackTimeout, r.Metrics.OpenstackMetrics)
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
		}).
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

	err := r.Client.Get(ctx, client.ObjectKey{Name: sa.Name, Namespace: sa.Namespace}, &sa)
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

func (r *LoadBalancerMachineReconciler) reconcilePort(
	ctx context.Context,
	osClient os.Client,
	req ctrl.Request,
	lbm *yawolv1beta1.LoadBalancerMachine,
	lb *yawolv1beta1.LoadBalancer,
) error {
	var err error

	if lbm.Spec.Infrastructure.NetworkID == "" {
		return helper.ErrNoNetworkID
	}

	var portClient os.PortClient
	portClient, err = osClient.PortClient(ctx)
	if err != nil {
		return err
	}

	// create port
	var port *ports.Port
	if lbm.Status.PortID == nil {
		port, err = openstackhelper.CreatePort(ctx, portClient, req.NamespacedName.String(), lbm.Spec.Infrastructure.NetworkID)
		if err != nil {
			r.Log.Info("unexpected error occurred claiming a port", "lbm", lbm.Name)
			return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
		}

		// double check so status won't be corrupted
		if port.ID == "" {
			r.Log.Info(helper.ErrPortIDEmpty.Error(), "lbm", lbm.Name)
			return helper.ErrPortIDEmpty
		}

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
			yawolv1beta1.LoadBalancerMachineStatus{PortID: &port.ID},
		); err != nil {
			return err
		}
	}

	// Check if port still exists properly
	if lbm.Status.PortID != nil {
		if port, err = openstackhelper.GetPortByID(ctx, portClient, *lbm.Status.PortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("lbm port not found in openstack", "portID", *lbm.Status.PortID)
				// remove port from LB status so a new one gets created next reconcile
				if err := helper.RemoveFromLBMStatus(ctx, r.Client.Status(), lbm, "portID"); err != nil {
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

	if lbm.Status.PortID == nil {
		r.Log.Info(helper.ErrLBMPortNotSet.Error(), "lbm", lbm.Name)
		return "", helper.ErrLBMPortNotSet
	}

	portLBM, err := openstackhelper.GetPortByID(ctx, portClient, *lbm.Status.PortID)
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
	serviceAccount v1.ServiceAccount,
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
	if kubeconfig, err = r.getKubeConfigForServiceAccount(ctx, loadBalancerMachine.Namespace, serviceAccount); err != nil {
		return err
	}

	// Generate user-data for yawollet VM
	userData := helper.GenerateUserData(
		kubeconfig,
		loadbalancer.Name,
		loadBalancerMachine.Name,
		loadBalancerMachine.Namespace,
		loadbalancer.Spec.DebugSettings.Enabled,
		vip,
	)

	var srv *servers.Server

	srv, err = openstackhelper.GetServerByName(ctx, srvClient, loadBalancerMachine.Name)
	if err != nil {
		return err
	}

	// delete server if it is in error state
	if srv != nil && srv.Status == openstackhelper.ServerStatusError {
		return r.Client.Delete(ctx, loadBalancerMachine)
	}

	if loadBalancerMachine.Status.PortID == nil {
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
			return kubernetes.SendErrorAsEvent(r.Recorder, err, loadbalancer)
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
	imageID, err = helper.GetImageID(*loadBalancerMachine.Spec.Infrastructure.Image)
	if err != nil {
		return nil, err
	}

	var flavorID string
	flavorID, err = helper.GetFlavorID(*loadBalancerMachine.Spec.Infrastructure.Flavor)
	if err != nil {
		return nil, err
	}

	if loadBalancerMachine.Spec.Infrastructure.NetworkID == "" {
		return nil, helper.ErrNoNetworkID
	}

	var createOpts servers.CreateOptsBuilder
	createOpts = &servers.CreateOpts{
		Name:             loadBalancerMachine.Name,
		FlavorRef:        flavorID,
		ImageRef:         imageID,
		SecurityGroups:   nil,
		UserData:         []byte(userdata),
		AvailabilityZone: loadBalancerMachine.Spec.Infrastructure.AvailabilityZone,
		Networks:         []servers.Network{{UUID: loadBalancerMachine.Spec.Infrastructure.NetworkID, Port: *loadBalancerMachine.Status.PortID}},
		Metadata:         nil,
	}

	if loadBalancer.Spec.DebugSettings.Enabled {
		createOpts = &keypairs.CreateOptsExt{
			CreateOptsBuilder: createOpts,
			KeyName:           loadBalancer.Spec.DebugSettings.SshkeyName,
		}
	}

	var server *servers.Server
	server, err = openstackhelper.CreateServer(ctx, serverClient, createOpts)
	if err != nil {
		return nil, err
	}

	err = r.waitForServerStatus(ctx, serverClient, server.ID, []string{"BUILD"}, []string{"ACTIVE"}, 600)
	return server, err
}

func (r *LoadBalancerMachineReconciler) deleteServer(
	ctx context.Context,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	var err error

	var srvClient os.ServerClient
	srvClient, err = osClient.ServerClient(ctx)
	if err != nil {
		return err
	}

	var server *servers.Server
	server, err = openstackhelper.GetServerByName(ctx, srvClient, lbm.Name)
	if err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault404:
			r.Log.Info("error getting server, already deleted", "lbm", lbm.Name)
		default:
			r.Log.Info("an unexpected error occurred retrieving server", "lbm", lbm.Name)
			return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
		}
		return err
	}

	if server != nil && server.ID != "" {
		if err = openstackhelper.DeleteServer(ctx, srvClient, server.ID); err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("error deleting server, already deleted", "lbm", lbm.Name)
			default:
				r.Log.Info("an unexpected error occurred deleting server", "lbm", lbm.Name)
				return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
			}
		}
		err = r.waitForServerStatus(
			ctx,
			srvClient,
			server.ID,
			[]string{openstackhelper.ServerStatusActive, openstackhelper.ServerStatusStopped, openstackhelper.ServerStatusError},
			[]string{openstackhelper.ServerStatusDeleted},
			600,
		)
		if err != nil {
			return err
		}
	}

	if lbm.Status.ServerID != nil {
		if err = openstackhelper.DeleteServer(ctx, srvClient, *lbm.Status.ServerID); err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("error deleting server, already deleted", "lbm", lbm.Name)
			default:
				r.Log.Info("an unexpected error occurred deleting server", "lbm", lbm.Name)
				return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
			}
		}
		err = r.waitForServerStatus(
			ctx,
			srvClient,
			*lbm.Status.ServerID,
			[]string{openstackhelper.ServerStatusActive, openstackhelper.ServerStatusStopped, openstackhelper.ServerStatusError},
			[]string{openstackhelper.ServerStatusDeleted},
			600,
		)
		if err != nil {
			return err
		}
	}

	err = helper.RemoveFromLBMStatus(ctx, r.Status(), lbm, "serverID")
	if err != nil {
		return err
	}
	return nil
}

func (r *LoadBalancerMachineReconciler) deletePort(
	ctx context.Context,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
) error {
	if lbm.Status.PortID == nil {
		r.Log.Info("no port associated with this machine, already deleted")
		return nil
	}

	var err error
	var portClient os.PortClient
	portClient, err = osClient.PortClient(ctx)
	if err != nil {
		return err
	}

	if err = openstackhelper.DeletePort(ctx, portClient, *lbm.Status.PortID); err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault404:
			r.Log.Info("error deleting port, already deleted", "lbm", lbm.Name)
		default:
			r.Log.Info("an unexpected error occurred deleting port", "lbm", lbm.Name)
			return kubernetes.SendErrorAsEvent(r.Recorder, err, lbm)
		}
	}

	return nil
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
	if err := r.Client.Delete(ctx, &sa); client.IgnoreNotFound(err) != nil {
		return err
	}

	return helper.RemoveFromLBMStatus(ctx, r.Status(), lbm, "serviceAccountName")
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
	if err := r.Client.Delete(ctx, &rb); client.IgnoreNotFound(err) != nil {
		return err
	}

	return helper.RemoveFromLBMStatus(ctx, r.Status(), lbm, "roleBindingName")
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
	if err := r.Client.Delete(ctx, &role); client.IgnoreNotFound(err) != nil {
		return err
	}

	return helper.RemoveFromLBMStatus(ctx, r.Status(), lbm, "roleName")
}

func (r *LoadBalancerMachineReconciler) waitForServerStatus(
	ctx context.Context,
	serverClient os.ServerClient,
	id string,
	pending []string,
	target []string,
	secs int,
) error {
	return gophercloud.WaitFor(secs, func() (bool, error) {
		current, err := openstackhelper.GetServerByID(ctx, serverClient, id)
		if err != nil {
			if _, ok := err.(gophercloud.ErrDefault404); ok && slices.Contains(target, "DELETED") {
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
	namespace string,
	sa v1.ServiceAccount,
) (string, error) {
	if len(sa.Secrets) < 1 {
		return "", fmt.Errorf("%w for serviceAccount %s", helper.ErrSecretNotFound, sa.Name)
	}

	sec := v1.Secret{}
	var err error
	if err = r.Client.Get(ctx, types.NamespacedName{Name: sa.Secrets[0].Name, Namespace: namespace}, &sec); err != nil {
		return "", fmt.Errorf("%w cloud not getting %s", helper.ErrSecretNotFound, sec.Name)
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
			Namespace: namespace,
		}},
		CurrentContext: "context",
	}

	var kubeConfig []byte
	if kubeConfig, err = runtime.Encode(latest.Codec, &config); err != nil {
		return "", err
	}

	return string(kubeConfig), nil
}
