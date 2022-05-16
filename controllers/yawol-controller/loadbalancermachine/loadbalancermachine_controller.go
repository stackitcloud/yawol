package loadbalancermachine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	os "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/internal/openstack"

	"github.com/prometheus/client_golang/prometheus"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ServiceFinalizer Name of finalizer for controller4
	ServiceFinalizer = "yawol.stackit.cloud/controller4"
	serverError      = "ERROR"
)

var ipv4Regex = `^(((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.|$)){4})`
var ipv4RegexC, _ = regexp.Compile(ipv4Regex)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerMachineReconciler struct {
	client.Client
	APIHost           string
	CACert            []byte
	Log               logr.Logger
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	RecorderLB        record.EventRecorder
	ApiEndpoint       string
	MachineMetrics    prometheus.GaugeVec
	OpenstackMetrics  prometheus.CounterVec
	getOsClientForIni func(iniData []byte) (os.Client, error)
	WorkerCount       int
	OpenstackTimeout  time.Duration
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
// Reconcile Reconciles a LoadBalancerMachine
func (r *LoadBalancerMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancermachine", req.NamespacedName)

	var loadBalancerMachine yawolv1beta1.LoadBalancerMachine

	if err := r.Client.Get(ctx, req.NamespacedName, &loadBalancerMachine); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if loadBalancerMachine.Status.Metrics != nil {
		for _, metric := range *loadBalancerMachine.Status.Metrics {
			if value, err := strconv.ParseFloat(metric.Value, 64); err == nil {
				// metric labels: type, lb, lbm, namespace
				r.MachineMetrics.WithLabelValues(metric.Type,
					loadBalancerMachine.Spec.LoadBalancerRef.Name,
					loadBalancerMachine.Name,
					req.Namespace).Set(value)
			}
		}
	}

	// check if reconcile is needed
	before5Minutes := metav1.Time{Time: time.Now().Add(-5 * time.Minute)}
	if loadBalancerMachine.Status.LastOpenstackReconcile != nil &&
		!loadBalancerMachine.Status.LastOpenstackReconcile.Before(&before5Minutes) &&
		loadBalancerMachine.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	loadBalancerMachineName := loadBalancerMachine.Name
	loadBalancerName := loadBalancerMachine.Spec.LoadBalancerRef.Name
	loadBalancerNamespace := loadBalancerMachine.Spec.LoadBalancerRef.Namespace
	secretRef := loadBalancerMachine.Spec.Infrastructure.AuthSecretRef

	var imageID string
	{
		var err error
		imageID, err = getImageID(*loadBalancerMachine.Spec.Infrastructure.Image)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	var flavorID string
	{
		var err error
		flavorID, err = getFlavorID(*loadBalancerMachine.Spec.Infrastructure.Flavor)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	networkID := loadBalancerMachine.Spec.Infrastructure.NetworkID

	// get loadbalancer for debug settings
	var loadbalancer yawolv1beta1.LoadBalancer
	if err := r.Client.Get(
		ctx,
		client.ObjectKey{Name: loadBalancerName, Namespace: loadBalancerNamespace},
		&loadbalancer,
	); err != nil {
		return ctrl.Result{}, err
	}

	var sshkey string
	if loadbalancer.Spec.DebugSettings.Enabled {
		if loadbalancer.Spec.DebugSettings.SshkeyName != "" {
			sshkey = loadbalancer.Spec.DebugSettings.SshkeyName
		}
	}

	var infraSecret v1.Secret
	if err := r.Client.Get(ctx, client.ObjectKey{
		Name:      secretRef.Name,
		Namespace: secretRef.Namespace,
	}, &infraSecret); err != nil {
		r.Log.Error(err, "could not get infrastructure secret")
		return ctrl.Result{}, err
	}

	if _, ok := infraSecret.Data["cloudprovider.conf"]; !ok {
		return ctrl.Result{}, fmt.Errorf("cloudprovider.conf not found in secret")
	}

	var osClient os.Client
	{
		var err error
		if osClient, err = r.getOsClientForIni(infraSecret.Data["cloudprovider.conf"]); err != nil {
			return ctrl.Result{}, err
		}
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if loadBalancerMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !hasFinalizer(loadBalancerMachine.ObjectMeta, ServiceFinalizer) {
			r.Log.Info("Add finalizer", "loadBalancerMachineName", loadBalancerMachineName)
			if err := r.addFinalizer(ctx, loadBalancerMachine, ServiceFinalizer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		// The object is being deleted
		if hasFinalizer(loadBalancerMachine.ObjectMeta, ServiceFinalizer) {
			// check if server is still running
			var srv servers.Server
			{
				var err error
				srv, err = r.checkServer(ctx, loadBalancerMachineName, osClient)
				if err != nil {
					return ctrl.Result{}, err
				}
			}

			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteServer(ctx, osClient, srv); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.deletePort(ctx, osClient, loadBalancerMachine.Status.PortID); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.deleteSA(&ctx, req.Namespace, loadBalancerMachineName); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.deleteRoleBinding(&ctx, req.Namespace, loadBalancerMachineName); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.deleteRole(&ctx, req.Namespace, loadBalancerMachineName); err != nil {
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			r.Log.Info("Remove finalizer", "loadBalancerMachineName", loadBalancerMachineName)
			if err := r.removeFinalizer(ctx, loadBalancerMachine, ServiceFinalizer); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	secGroupID := loadbalancer.Status.SecurityGroupID
	if secGroupID == nil {
		return ctrl.Result{}, fmt.Errorf("loadbalancer SecurityGroupID is nil %v", loadbalancer)
	}

	if *secGroupID == "" ||
		imageID == "" ||
		flavorID == "" ||
		networkID == "" {
		return ctrl.Result{}, fmt.Errorf(
			"some fields are missing for server creation in CR, SecGroupID: %v, imageID: %v, flavorID: %v, networkID %v",
			*secGroupID, imageID, flavorID, networkID,
		)
	}

	// Reconcile ServiceAccount for yawollet access
	var sa v1.ServiceAccount
	{
		var err error
		var res ctrl.Result
		if res, sa, err = r.reconcileSA(&ctx, req.Namespace, loadBalancerMachineName); err != nil || res.Requeue || res.RequeueAfter != 0 {
			return res, err
		}
	}

	// Reconcile Role for yawollet access
	if err := r.reconcileRole(&ctx, req.Namespace, loadBalancerName, loadBalancerMachineName); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile RoleBinding for yawollet access
	if err := r.reconcileRoleBinding(&ctx, req.Namespace, loadBalancerMachineName); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile Kubeconfig which will be passed to VM user-data for yawollet access
	var kubeconfig string
	{
		var err error
		var res ctrl.Result
		if res, kubeconfig, err = r.reconcileKubeConfig(&ctx, req.Namespace, sa); err != nil || res.Requeue || res.RequeueAfter != 0 {
			return res, err
		}
	}

	var srv servers.Server
	{
		var err error
		srv, err = r.checkServer(ctx, loadBalancerMachineName, osClient)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if srv.Status == serverError {
		return ctrl.Result{}, r.Client.Delete(ctx, &loadBalancerMachine)
	}

	if err := r.reconcilePort(
		ctx, req, osClient,
		&loadBalancerMachine,
		loadbalancer.Spec.Infrastructure.NetworkID,
		*secGroupID,
	); err != nil {
		return ctrl.Result{}, err
	}

	var vip string
	{
		var err error
		vip, err = r.reconcilePortAddressPair(ctx, req, osClient, &loadBalancerMachine, &loadbalancer)

		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Generate user-data for yawollet VM
	userData := generateUserData(
		kubeconfig,
		loadBalancerName,
		loadBalancerMachineName,
		req.Namespace,
		loadbalancer.Spec.DebugSettings.Enabled,
		vip,
	)

	if srv.ID == "" && loadBalancerMachine.Status.PortID != nil {
		serverNetworks := []servers.Network{{UUID: networkID, Port: *loadBalancerMachine.Status.PortID}}

		var err error
		srv, err = r.createServer(ctx, osClient, loadBalancerMachineName, serverNetworks, imageID, flavorID, "eu01-m", userData, sshkey)
		if err != nil {
			var lb yawolv1beta1.LoadBalancer
			lberr := r.Client.Get(ctx, types.NamespacedName{Name: loadBalancerName, Namespace: loadBalancerNamespace}, &lb)
			if lberr != nil {
				r.Log.Error(lberr, fmt.Sprintf("lb not found: %s", loadBalancerName))
			}
			return ctrl.Result{}, r.sendEvent(err, &loadBalancerMachine, &lb)
		}
	}

	if loadBalancerMachine.Status.ServerID == nil || srv.ID != *loadBalancerMachine.Status.ServerID {
		nowtime := metav1.Now()
		if err := r.patchLBMStatus(ctx, &loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
			Conditions:        nil,
			Metrics:           nil,
			CreationTimestamp: &nowtime,
			ServerID:          &srv.ID,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	nowtime := metav1.Now()
	if err := r.patchLBMStatus(ctx, &loadBalancerMachine, yawolv1beta1.LoadBalancerMachineStatus{
		LastOpenstackReconcile: &nowtime,
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
			err := osClient.Configure(iniData, r.OpenstackTimeout)
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

func getImageID(spec yawolv1beta1.OpenstackImageRef) (string, error) {
	if spec.ImageID != nil {
		return *spec.ImageID, nil
	}
	if spec.ImageName != nil {
		// TODO get imageid from name
		return "", nil
	}
	if spec.ImageSearch != nil {
		// TODO get imageid by metadata search
		return "", nil
	}
	return "", fmt.Errorf("imageid not found")
}

func getFlavorID(spec yawolv1beta1.OpenstackFlavorRef) (string, error) {
	if spec.FlavorID != nil {
		return *spec.FlavorID, nil
	}
	if spec.FlavorName != nil {
		// TODO get flavorid from name
		return "", nil
	}
	if spec.FlavorSearch != nil {
		// TODO get flavorid by metadata search
		return "", nil
	}
	return "", fmt.Errorf("flavorid not found")
}

func (r *LoadBalancerMachineReconciler) checkServer(
	ctx context.Context, name string, osClient os.Client,
) (servers.Server, error) {
	r.Log.Info("Check server", "loadBalancerMachineName", name)
	var server servers.Server
	var srvClient os.ServerClient
	{
		var err error
		srvClient, err = osClient.ServerClient(ctx)
		if err != nil {
			return servers.Server{}, err
		}
	}
	opts := servers.ListOpts{Name: name}
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	serverList, err := srvClient.List(tctx, opts)
	r.OpenstackMetrics.WithLabelValues("Nova").Inc()
	if err != nil {
		return servers.Server{}, fmt.Errorf("error extract server, %s", err)
	}

	for _, p := range serverList {
		if p.Name == name {
			server = p
		}
	}

	return server, err
}

func (r *LoadBalancerMachineReconciler) createServer(
	ctx context.Context,
	osClient os.Client,
	loadBalancerMachineName string,
	serverNetworks []servers.Network,
	imageID string,
	flavorID string,
	availabilityZone string,
	userdata string,
	key string,
) (servers.Server, error) {
	r.Log.Info("Create server", "loadBalancerMachineName", loadBalancerMachineName)
	var srvClient os.ServerClient
	var server *servers.Server
	{
		var err error
		srvClient, err = osClient.ServerClient(ctx)
		if err != nil {
			return servers.Server{}, err
		}
	}

	var createOpts servers.CreateOptsBuilder

	createOpts = &servers.CreateOpts{
		Name:             loadBalancerMachineName,
		FlavorRef:        flavorID,
		ImageRef:         imageID,
		SecurityGroups:   nil,
		UserData:         []byte(userdata),
		AvailabilityZone: availabilityZone,
		Networks:         serverNetworks,
		Metadata:         nil,
	}

	if key != "" {
		createOpts = &keypairs.CreateOptsExt{
			CreateOptsBuilder: createOpts,
			KeyName:           key,
		}
	}
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	var err error
	server, err = srvClient.Create(tctx, createOpts)
	r.OpenstackMetrics.WithLabelValues("Nova").Inc()
	if err != nil {
		return servers.Server{}, err
	}

	err = r.waitForServerStatus(ctx, srvClient, server.ID, []string{"BUILD"}, []string{"ACTIVE"}, 600)
	return *server, err
}

func (r *LoadBalancerMachineReconciler) deleteServer(ctx context.Context, osClient os.Client, server servers.Server) error {
	r.Log.Info("Delete server: ", "server", server.Name)
	var srvClient os.ServerClient
	{
		var err error
		srvClient, err = osClient.ServerClient(ctx)
		if err != nil {
			return err
		}
	}

	var err error
	r.OpenstackMetrics.WithLabelValues("Nova").Inc()
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	if err = srvClient.Delete(tctx, server.ID); err != nil {
		if _, ok := err.(gophercloud.ErrDefault404); ok {
			return nil
		}
		return err
	}

	err = r.waitForServerStatus(ctx, srvClient, server.ID, []string{"ACTIVE", "STOPPED", serverError}, []string{"DELETED"}, 600)
	if err != nil {
		return err
	}
	return nil
}

func (r *LoadBalancerMachineReconciler) reconcilePort(
	ctx context.Context,
	req ctrl.Request,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
	networkID string,
	secGroupID string,
) error {
	var portClient os.PortClient
	{
		var err error
		portClient, err = osClient.PortClient(ctx)
		if err != nil {
			return err
		}
	}

	// create port
	var port *ports.Port
	if lbm.Status.PortID == nil {
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		var err error
		port, err = portClient.Create(tctx, ports.CreateOpts{
			Name:      req.NamespacedName.String(),
			NetworkID: networkID,
			// same security group as loadbalancer port
			SecurityGroups: &[]string{secGroupID},
		})
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			r.Log.Info("unexpected error occurred claiming a port", "lbm", req.NamespacedName)
			return r.sendEvent(err, lbm)
		}

		// double check so status wont be corrupted
		if port.ID == "" {
			r.Log.Info("port was successfully created but port id is empty", "lbm", req.NamespacedName)
			panic("port was successfully created but port id is empty. crashing to prevent corruption of state")
		}

		r.Log.Info("successfully created port", "id", port.ID, "lbm", req.NamespacedName)

		if err = r.patchLBMStatus(
			ctx, lbm,
			yawolv1beta1.LoadBalancerMachineStatus{PortID: &port.ID},
		); err != nil {
			return err
		}
	}

	// Check if port still exists properly
	if lbm.Status.PortID != nil {
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		var err error
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if port, err = portClient.Get(tctx, *lbm.Status.PortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("lbm port not found in openstack", "portID", *lbm.Status.PortID)
				// remove port from LB status so a new one gets created next reconcile
				if err = r.removeFromLBMStatus(ctx, lbm, "portID"); err != nil {
					return err
				}

				return r.sendEvent(err, lbm)
			default:
				r.Log.Info("unexpected error while fetching ports occurred")
				return r.sendEvent(err, lbm)
			}
		}
	}

	if port == nil {
		return errors.New("failed creating port for lbm")
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) reconcilePortAddressPair(
	ctx context.Context,
	req ctrl.Request,
	osClient os.Client,
	lbm *yawolv1beta1.LoadBalancerMachine,
	lb *yawolv1beta1.LoadBalancer,
) (string, error) {
	var portClient os.PortClient
	{
		var err error
		portClient, err = osClient.PortClient(ctx)
		if err != nil {
			return "", err
		}
	}

	if lbm.Status.PortID == nil {
		message := "load balancer machine port not set"
		r.Log.Info(message, "lbm", req.NamespacedName)
		return "", errors.New(message)
	}

	if lb.Status.PortID == nil {
		message := "load balancer port not set"
		r.Log.Info(message, "lb", req.NamespacedName)
		return "", errors.New(message)
	}

	fetchPort := func(obj runtime.Object, id string) (*ports.Port, error) {
		var port *ports.Port
		var err error
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()

		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		if port, err = portClient.Get(tctx, id); err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("port not found in openstack", "portID", id)
				return nil, r.sendEvent(err, obj)
			default:
				r.Log.Info("unexpected error while fetching ports occurred")
				return nil, r.sendEvent(err, obj)
			}
		}

		if port == nil {
			return nil, errors.New("lb or lbm ports are nill")
		}

		return port, nil
	}

	portLBM, err := fetchPort(lbm, *lbm.Status.PortID)
	if err != nil {
		return "", err
	}

	portLB, err := fetchPort(lb, *lb.Status.PortID)
	if err != nil {
		return "", err
	}

	if len(portLB.FixedIPs) < 1 {
		return "", errors.New("no fixed ip for loadbalancer port")
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
		return "", errors.New("no ipv4 in fixed ips for loadbalancer port")
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
			// port adresspairs are already up to date, return only ipv4 until dualstack is implemented
			return ipv4, nil
		}
	}

	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	_, err = portClient.Update(tctx, portLBM.ID, ports.UpdateOpts{
		AllowedAddressPairs: &addressPair,
	})

	return ipv4, err
}

func (r *LoadBalancerMachineReconciler) deletePort(
	ctx context.Context,
	osClient os.Client,
	portID *string,
) error {
	if portID == nil {
		r.Log.Info("no port associated with this machine, already deleted")
		return nil
	}

	var portClient os.PortClient
	{
		var err error
		portClient, err = osClient.PortClient(ctx)
		if err != nil {
			return err
		}
	}

	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	if err := portClient.Delete(tctx, *portID); err != nil {
		if _, ok := err.(gophercloud.ErrDefault404); ok {
			return nil
		}

		return err
	}

	return nil
}

func hasFinalizer(objectMeta metav1.ObjectMeta, finalizer string) bool {
	for _, f := range objectMeta.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func (r *LoadBalancerMachineReconciler) addFinalizer(
	ctx context.Context,
	loadBalancerMachine yawolv1beta1.LoadBalancerMachine,
	finalizer string,
) error {
	patch := []byte(`{"metadata":{"finalizers": ["` + finalizer + `"]}}`)
	return r.Client.Patch(ctx, &loadBalancerMachine, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerMachineReconciler) removeFinalizer(
	ctx context.Context,
	loadBalancerMachine yawolv1beta1.LoadBalancerMachine,
	finalizer string,
) error {
	fin, err := json.Marshal(removeString(loadBalancerMachine.Finalizers, finalizer))
	if err != nil {
		return err
	}
	patch := []byte(`{"metadata":{"finalizers": ` + string(fin) + ` }}`)
	return r.Client.Patch(ctx, &loadBalancerMachine, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerMachineReconciler) waitForServerStatus(
	ctx context.Context,
	srvClient os.ServerClient,
	id string,
	pending []string,
	target []string,
	secs int,
) error {
	return gophercloud.WaitFor(secs, func() (bool, error) {
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		current, err := srvClient.Get(tctx, id)
		if err != nil {
			if current != nil && current.Name != "" {
				r.OpenstackMetrics.WithLabelValues("Nova").Inc()
			}
			if _, ok := err.(gophercloud.ErrDefault404); ok && strSliceContains(target, "DELETED") {
				return true, nil
			}
			return false, err
		}

		if strSliceContains(target, current.Status) {
			return true, nil
		}

		// if there is no pending statuses defined or current status is in the pending list, then continue polling
		if len(pending) == 0 || strSliceContains(pending, current.Status) {
			return false, nil
		}

		retErr := fmt.Errorf("unexpected status %q, wanted target %q", current.Status, strings.Join(target, ", "))
		if current.Status == serverError {
			retErr = fmt.Errorf("%s, fault: %+v", retErr, current.Fault)
		}

		return false, retErr
	})
}

func strSliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return result
}

func (r *LoadBalancerMachineReconciler) patchLBMStatus(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
	lbStatus yawolv1beta1.LoadBalancerMachineStatus,
) error {
	lbStatusJson, _ := json.Marshal(lbStatus)
	patch := []byte(`{"status":` + string(lbStatusJson) + `}`)
	return r.Client.Status().Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerMachineReconciler) removeFromLBMStatus(
	ctx context.Context,
	lbm *yawolv1beta1.LoadBalancerMachine,
	key string,
) error {
	patch := []byte(`{"status":{"` + key + `": null}}`)
	return r.Client.Status().Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerMachineReconciler) reconcileKubeConfig(
	ctx *context.Context,
	ns string,
	sa v1.ServiceAccount,
) (ctrl.Result, string, error) {
	if len(sa.Secrets) < 1 {
		r.Log.Info("secret for sa not found, requeue")
		return ctrl.Result{Requeue: true}, "", nil
	}

	sec := v1.Secret{}
	var err error
	if err = r.Client.Get(*ctx, types.NamespacedName{Name: sa.Secrets[0].Name, Namespace: ns}, &sec); err != nil {
		return ctrl.Result{}, "", fmt.Errorf("error getting secret, %v, %s", sec, err)
	}

	var token, ca []byte
	var ok bool

	if token, ok = sec.Data["token"]; !ok {
		return ctrl.Result{}, "", fmt.Errorf("token not found in secret, %v", sec)
	}

	if ca, ok = sec.Data["ca.crt"]; !ok {
		return ctrl.Result{}, "", fmt.Errorf("ca.crt not found in secret, %v", sec)
	}

	config := api.Config{
		Kind:        "Config",
		APIVersion:  "v1",
		Preferences: api.Preferences{},
		Clusters: map[string]*api.Cluster{"default-cluster": {
			Server:                   r.ApiEndpoint,
			InsecureSkipTLSVerify:    false,
			CertificateAuthority:     "",
			CertificateAuthorityData: ca,
		}},
		AuthInfos: map[string]*api.AuthInfo{"user": {
			Token: string(token),
		}},
		Contexts: map[string]*api.Context{"context": {
			Cluster:    "default-cluster",
			AuthInfo:   "user",
			Namespace:  ns,
			Extensions: nil,
		}},
		CurrentContext: "context",
		Extensions:     nil,
	}

	r.Log.Info("encoding kubeconfig", "codec", latest.Codec, "config", config, "secret", sec)
	var c []byte
	if c, err = runtime.Encode(latest.Codec, &config); err != nil {
		return ctrl.Result{}, "", err
	}

	return ctrl.Result{}, string(c), nil
}

func (r *LoadBalancerMachineReconciler) reconcileSA(
	ctx *context.Context,
	namespace string,
	name string,
) (ctrl.Result, v1.ServiceAccount, error) {
	r.Log.Info("Check SA", "loadBalancerMachineName", name)

	sa := v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := r.Client.Get(*ctx, client.ObjectKey{Name: sa.Name, Namespace: sa.Namespace}, &sa)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, v1.ServiceAccount{}, err
	}

	if err != nil && client.IgnoreNotFound(err) == nil {
		if err := r.Client.Create(*ctx, &sa); err != nil {
			return ctrl.Result{}, v1.ServiceAccount{}, fmt.Errorf("error creating sa, %s", err)
		}
		r.Log.Info("SA created", "loadBalancerMachineName", name)
		return ctrl.Result{Requeue: true}, v1.ServiceAccount{}, nil
	}

	return ctrl.Result{}, sa, nil
}

func (r *LoadBalancerMachineReconciler) deleteSA(ctx *context.Context, namespace string, name string) error {
	sa := v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := r.Client.Delete(*ctx, &sa); err != nil {
		return client.IgnoreNotFound(err)
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) reconcileRole(
	ctx *context.Context,
	namespace string,
	loadBalancerName string,
	loadBalancerMachineName string,
) error {
	r.Log.Info("Check role", "loadBalancerMachineName", loadBalancerMachineName)

	rules := []rbac.PolicyRule{{
		Verbs:     []string{"create"},
		APIGroups: []string{""},
		Resources: []string{"events"},
	}, {
		Verbs:     []string{"list", "watch"},
		APIGroups: []string{"yawol.stackit.cloud"},
		Resources: []string{"loadbalancers"},
	}, {
		Verbs:         []string{"get"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancers"},
		ResourceNames: []string{loadBalancerName},
	}, {
		Verbs:     []string{"list", "watch"},
		APIGroups: []string{"yawol.stackit.cloud"},
		Resources: []string{"loadbalancermachines"},
	}, {
		Verbs:         []string{"get", "update", "patch"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancermachines"},
		ResourceNames: []string{loadBalancerMachineName},
	}, {
		Verbs:         []string{"get", "update", "patch"},
		APIGroups:     []string{"yawol.stackit.cloud"},
		Resources:     []string{"loadbalancermachines/status"},
		ResourceNames: []string{loadBalancerMachineName},
	}}

	role := rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadBalancerMachineName,
			Namespace: namespace,
		},
		Rules: rules,
	}
	err := r.Client.Get(*ctx, client.ObjectKey{Name: role.Name, Namespace: role.Namespace}, &role)
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	if err != nil && client.IgnoreNotFound(err) == nil {
		if err := r.Client.Create(*ctx, &role); err != nil {
			return err
		}

		r.Log.Info("Role created", "loadBalancerMachineName", loadBalancerMachineName)
	}

	return nil
}

func (r *LoadBalancerMachineReconciler) reconcileRoleBinding(
	ctx *context.Context,
	namespace string,
	machinename string,
) error {
	r.Log.Info("Check rolebinding", "loadBalancerMachineName", machinename)

	rb := rbac.RoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      machinename,
			Namespace: namespace,
		},
		Subjects: []rbac.Subject{{
			Kind:      "ServiceAccount",
			Name:      machinename,
			Namespace: namespace,
		}},
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "Role",
			Name:     machinename,
		},
	}

	err := r.Client.Get(*ctx, client.ObjectKey{Name: rb.Name, Namespace: rb.Namespace}, &rb)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if err != nil && client.IgnoreNotFound(err) == nil {
		if err := r.Client.Create(*ctx, &rb); err != nil {
			return err
		}
		r.Log.Info("rolebinding created", "loadBalancerMachineName", machinename)
	}
	return nil
}

func (r *LoadBalancerMachineReconciler) deleteRole(ctx *context.Context, namespace string, name string) error {
	role := rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := r.Client.Delete(*ctx, &role); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

func (r *LoadBalancerMachineReconciler) deleteRoleBinding(ctx *context.Context, namespace string, name string) error {
	rb := rbac.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := r.Client.Delete(*ctx, &rb); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

func generateUserData(
	kubeconfig string,
	loadBalancerName string,
	loadBalancerMachineName string,
	namespace string,
	debug bool,
	vip string,
) string {
	bk := base64.StdEncoding.EncodeToString([]byte(kubeconfig))
	keepalivedConfig := base64.StdEncoding.EncodeToString(
		[]byte(generateKeepalivedConfig(vip)),
	)

	var systemctlSshd, openrcSshd, openrcState string
	if debug {
		systemctlSshd = "enable"
		openrcSshd = "add"
		openrcState = "start"
	} else {
		systemctlSshd = "disable"
		openrcSshd = "del"
		openrcState = "stop"
	}

	tpl := `
#cloud-config
write_files:
- encoding: b64
  content: ` + bk + `
  owner: yawol:yawol
  path: /etc/yawol/kubeconfig
  permissions: '0600'
- encoding: b64
  content: ` + keepalivedConfig + `
  owner: root:root
  path: /etc/keepalived/keepalived.conf
  permissions: '0644'
- content: >
    YAWOLLET_ARGS="-namespace=` + namespace + `
    -loadbalancer-name=` + loadBalancerName + `
    -loadbalancer-machine-name=` + loadBalancerMachineName + `
    -listen-address=` + vip + `
    -kubeconfig /etc/yawol/kubeconfig"
  path: /etc/yawol/env.conf
runcmd:
  - [ /sbin/rc-service, sshd, ` + openrcState + ` ]
  - [ /sbin/rc-update, ` + openrcSshd + `, sshd, default ]
  - [ systemctl, ` + systemctlSshd + `, ssh.service, --now ]
  - [ systemctl, ` + systemctlSshd + `, sshd.service, --now ]
  - [ systemctl, daemon-reload ]
  - [ systemctl, restart, keepalived.service ]
  - [ systemctl, restart, yawollet.service ]
  - [ systemctl, restart, envoy.service ]
  - [ /sbin/rc-service, keepalived, restart ]
  - [ /sbin/rc-service, envoy, restart ]
  - [ /sbin/rc-service, yawollet, restart ]
`
	return tpl
}

func generateKeepalivedConfig(vip string) string {
	return `
! Configuration File for keepalived

global_defs {
	router_id envoy
	max_auto_priority -1
}

vrrp_track_process envoy {
	process envoy
	weight 100
}

vrrp_instance ENVOY {
	state MASTER
	interface eth0
	virtual_router_id 100
	priority 100
	advert_int 1

	authentication {
		auth_type PASS
		auth_pass yawol
	}

	virtual_ipaddress {
		` + vip + `
	}

	track_process {
		envoy
	}
}
	`
}

func (r *LoadBalancerMachineReconciler) sendEvent(err error, obj ...runtime.Object) error {
	var body []byte
	switch casted := err.(type) {
	case gophercloud.ErrDefault401:
		body = casted.Body
	case gophercloud.ErrDefault403:
		body = casted.Body
	case gophercloud.ErrDefault404:
		body = casted.Body
	}

	if len(body) > 0 {
		for _, ob := range obj {
			if ob.GetObjectKind().GroupVersionKind().Kind == "LoadBalancer" {
				r.RecorderLB.Event(ob, v1.EventTypeWarning, "Failed", string(body))
			} else {
				r.Recorder.Event(ob, v1.EventTypeWarning, "Failed", string(body))
			}
		}
	}
	return err
}
