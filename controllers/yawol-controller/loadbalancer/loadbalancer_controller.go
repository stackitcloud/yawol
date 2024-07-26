package loadbalancer

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"
	"github.com/stackitcloud/yawol/internal/openstack"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servergroups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	openstackhelper "github.com/stackitcloud/yawol/internal/helper/openstack"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
)

const (
	DefaultRequeueTime = 10 * time.Millisecond
	ServiceFinalizer   = "yawol.stackit.cloud/controller2"
)

// LoadBalancerReconciler reconciles service Objects with type LoadBalancer
type Reconciler struct {
	client.Client
	Log               logr.Logger
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	RecorderLB        record.EventRecorder
	skipReconciles    bool
	skipAllButNN      *types.NamespacedName
	Metrics           *helpermetrics.LoadBalancerMetricList
	getOsClientForIni openstack.GetOSClientFunc
	WorkerCount       int
	OpenstackTimeout  time.Duration
	RateLimiter       ratelimiter.RateLimiter
}

// Reconcile function for LoadBalancer object
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if r.skipReconciles || (r.skipAllButNN != nil && r.skipAllButNN.Name != req.Name && r.skipAllButNN.Namespace != req.Namespace) {
		r.Log.Info("Skip reconciles enabled.. requeuing", "lb", req.NamespacedName)
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// get LoadBalancer object for this Reconcile request
	var lb yawolv1beta1.LoadBalancer
	if err := r.Client.Get(ctx, req.NamespacedName, &lb); err != nil {
		// If not found just add an info log and ignore error
		if errors2.IsNotFound(err) {
			r.Log.Info("LoadBalancer not found", "lb", req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(fmt.Errorf("failed to get LB: %w", err))
	}

	if err := r.migrateAPI(ctx, &lb); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to migrateAPI: %w", err)
	}

	// get OpenStack Client for LoadBalancer
	osClient, err := openstackhelper.GetOpenStackClientForInfrastructure(ctx, r.Client, lb.Spec.Infrastructure, r.getOsClientForIni)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get open stack client for infrastructure: %w", err)
	}

	var res ctrl.Result

	// handle deletion
	if lb.GetDeletionTimestamp() != nil {
		if res, err := r.deletionRoutine(ctx, &lb, osClient); err != nil || res.Requeue || res.RequeueAfter != 0 {
			if err != nil {
				return res, fmt.Errorf("failed to run deletion routine: %w", err)
			}
			return res, nil
		}
		return ctrl.Result{}, nil
	}

	// update metrics
	helper.ParseLoadBalancerMetrics(
		lb,
		r.Metrics,
	)

	// adds finalizer if not set
	if err := kubernetes.AddFinalizerIfNeeded(ctx, r.Client, &lb, ServiceFinalizer); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
	}

	emptyResult := ctrl.Result{}

	// run openstack reconcile if needed
	if res, err = r.reconcileOpenStackIfNeeded(ctx, &lb, req, osClient); err != nil || res != emptyResult {
		if err != nil {
			return res, fmt.Errorf("failed to reconcile openstack: %w", err)
		}
		return res, nil
	}

	// lbs reconcile is not affected by lastOpenstackReconcile
	if res, err := r.reconcileLoadBalancerSet(ctx, log, &lb); err != nil || res != emptyResult {
		if err != nil {
			return res, fmt.Errorf("failed to reconcile loadBalancerSet: %w", err)
		}
		return res, nil
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.getOsClientForIni == nil {
		r.getOsClientForIni = func(iniData []byte, overwrite openstack.OSClientOverwrite) (openstack.Client, error) {
			osClient := openstack.OSClient{}
			err := osClient.Configure(iniData, overwrite, r.OpenstackTimeout, r.Metrics.OpenstackMetrics)
			if err != nil {
				return nil, err
			}
			return &osClient, nil
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancer{}).
		Owns(&yawolv1beta1.LoadBalancerSet{}, builder.MatchEveryOwner, builder.WithPredicates(LoadBalancerSetPredicate())).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
			RateLimiter:             r.RateLimiter,
		}).
		Complete(r)
}

// LoadBalancerSetPredicate is the predicate that this controller uses for watching LoadBalancerSets.
func LoadBalancerSetPredicate() predicate.Predicate { // nolint: revive // this naming makes more sense
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			// On restart of the controller, we already reconcile all LoadBalancers.
			// When triggered by creation of a LoadBalancerSet by this controller, we don't need to reconcile immediately again.
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldLBS, ok := e.ObjectOld.(*yawolv1beta1.LoadBalancerSet)
			if !ok {
				return false
			}
			newLBS, ok := e.ObjectNew.(*yawolv1beta1.LoadBalancerSet)
			if !ok {
				return false
			}

			// this controller only needs to reconcile the LoadBalancer if a status of an owned LoadBalancerSet changes
			return !equality.Semantic.DeepEqual(oldLBS.Status, newLBS.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			lbs, ok := e.Object.(*yawolv1beta1.LoadBalancerSet)
			if !ok {
				return false
			}

			// a delete event is only relevant if a LoadBalancerSet was deleted that is supposed to have replicas or still has replicas
			// otherwise, a deletion doesn't need to be reconciled by this controller
			return lbs.Spec.Replicas > 0 ||
				ptr.Deref(lbs.Status.Replicas, 0) > 0 ||
				ptr.Deref(lbs.Status.ReadyReplicas, 0) > 0 ||
				ptr.Deref(lbs.Status.AvailableReplicas, 0) > 0
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}
}

//nolint:staticcheck // migration function
func (r *Reconciler) migrateAPI(ctx context.Context, lb *yawolv1beta1.LoadBalancer) error {
	if lb.Status.SecurityGroupIDOld != nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			SecurityGroupID:    lb.Status.SecurityGroupIDOld,
			SecurityGroupIDOld: nil,
		}); err != nil {
			return err
		}

		// needed because omitempty prevents setting the status to nil
		if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "security_group_id"); err != nil {
			return err
		}
	}

	if lb.Status.SecurityGroupNameOld != nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			SecurityGroupName:    lb.Status.SecurityGroupNameOld,
			SecurityGroupNameOld: nil,
		}); err != nil {
			return err
		}

		// needed because omitempty prevents setting the status to nil
		if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "security_group_name"); err != nil {
			return err
		}
	}

	if lb.Spec.Infrastructure.Flavor.FlavorIDOld != nil {
		lb.Spec.Infrastructure.Flavor.FlavorID = lb.Spec.Infrastructure.Flavor.FlavorIDOld
		lb.Spec.Infrastructure.Flavor.FlavorIDOld = nil
		if err := r.Client.Update(ctx, lb); err != nil {
			return err
		}
	}

	if lb.Spec.Infrastructure.Flavor.FlavorNameOld != nil {
		lb.Spec.Infrastructure.Flavor.FlavorName = lb.Spec.Infrastructure.Flavor.FlavorNameOld
		lb.Spec.Infrastructure.Flavor.FlavorNameOld = nil
		if err := r.Client.Update(ctx, lb); err != nil {
			return err
		}
	}

	if lb.Spec.Infrastructure.Flavor.FlavorSearchOld != nil {
		lb.Spec.Infrastructure.Flavor.FlavorSearch = lb.Spec.Infrastructure.Flavor.FlavorSearchOld
		lb.Spec.Infrastructure.Flavor.FlavorSearchOld = nil
		if err := r.Client.Update(ctx, lb); err != nil {
			return err
		}
	}

	if lb.Spec.Infrastructure.Image.ImageIDOld != nil {
		lb.Spec.Infrastructure.Image.ImageID = lb.Spec.Infrastructure.Image.ImageIDOld
		lb.Spec.Infrastructure.Image.ImageIDOld = nil
		if err := r.Client.Update(ctx, lb); err != nil {
			return err
		}
	}

	if lb.Spec.Infrastructure.Image.ImageNameOld != nil {
		lb.Spec.Infrastructure.Image.ImageName = lb.Spec.Infrastructure.Image.ImageNameOld
		lb.Spec.Infrastructure.Image.ImageNameOld = nil
		if err := r.Client.Update(ctx, lb); err != nil {
			return err
		}
	}

	if lb.Spec.Infrastructure.Image.ImageSearchOld != nil {
		lb.Spec.Infrastructure.Image.ImageSearch = lb.Spec.Infrastructure.Image.ImageSearchOld
		lb.Spec.Infrastructure.Image.ImageSearchOld = nil
		if err := r.Client.Update(ctx, lb); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reconciler) reconcileOpenStackIfNeeded(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	req ctrl.Request,
	osClient openstack.Client,
) (ctrl.Result, error) {
	if !helper.LoadBalancerOpenstackReconcileIsNeeded(lb) {
		return ctrl.Result{}, nil
	}

	r.Log.Info("Reconcile Openstack", "lb", lb.Name)

	var requeue, overallRequeue bool
	var err error

	requeue, err = r.reconcileSecGroup(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile sec group: %w", err)
	}
	overallRequeue = overallRequeue || requeue

	requeue, err = r.reconcileFIP(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile FIP: %w", err)
	}
	overallRequeue = overallRequeue || requeue

	requeue, err = r.reconcilePort(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile port: %w", err)
	}
	overallRequeue = overallRequeue || requeue

	requeue, err = r.reconcileFIPAssociate(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile FIP association: %w", err)
	}
	overallRequeue = overallRequeue || requeue

	if lb.Spec.Options.ServerGroupPolicy != "" {
		requeue, err = r.reconcileServerGroup(ctx, req, lb, osClient)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile server group: %w", err)
		}
		overallRequeue = overallRequeue || requeue
	} else {
		requeue, err = r.deleteServerGroups(ctx, osClient, lb)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete server groups: %w", err)
		}
		overallRequeue = overallRequeue || requeue
	}

	if overallRequeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
		LastOpenstackReconcile: &metaV1.Time{Time: time.Now()},
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch lb status: %w", err)
	}

	err = r.updateOpenstackReconcileHash(ctx, lb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update openstack reconcile hash: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) updateOpenstackReconcileHash(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
) error {
	// update status lb status accordingly
	openstackReconcileHash, err := helper.GetOpenStackReconcileHash(lb)
	if err != nil {
		return fmt.Errorf("failed to get reconcicle hash: %w", err)
	}

	if lb.Status.OpenstackReconcileHash != nil &&
		*lb.Status.OpenstackReconcileHash == openstackReconcileHash {
		return nil
	}

	if err = helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
		OpenstackReconcileHash: &openstackReconcileHash,
	}); err != nil {
		r.Log.Error(err, "failed to update OpenstackReconcileHash in status")
		return kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
	}

	return nil
}

func (r *Reconciler) reconcileFIP(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
	// delete fip if loadbalancer is internal
	if lb.Spec.Options.InternalLB {
		requeue, err := r.deleteFips(ctx, osClient, lb)
		if err != nil {
			return requeue, fmt.Errorf("failed to delete FIPs for internal LB: %w", err)
		}
		return requeue, nil
	}
	r.Log.Info("Reconcile FloatingIP", "lb", lb.Name)

	var err error

	// get openstack fip client
	var fipClient openstack.FipClient
	fipClient, err = osClient.FipClient(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to create FIP client: %w", err)
	}

	var requeue bool

	// Patch Floating Name, so we can reference it later
	if lb.Status.FloatingName == nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			FloatingName: ptr.To(req.NamespacedName.String()),
		}); err != nil {
			return false, fmt.Errorf("failed to patch lb status: %w", err)
		}
		requeue = true
	}

	if lb.Status.FloatingID == nil {
		if err := r.assignOrCreateFIP(ctx, fipClient, lb); err != nil {
			return false, fmt.Errorf("failed to assign FIP: %w", err)
		}
		requeue = true
	}

	// Get FIP
	var fip *floatingips.FloatingIP
	if fip, err = openstackhelper.GetFIPByID(ctx, fipClient, *lb.Status.FloatingID); err != nil {
		switch err.(type) {
		case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
			r.Log.Info("fip not found in openstack", "fip", *lb.Status.FloatingID)
			// fip not found by ID, remove it from status and trigger reconcile
			if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingID"); err != nil {
				return false, fmt.Errorf("failed to remove FIP from lb status: %w", err)
			}
			return true, err
		default:
			r.Log.Info("unexpected error occurred")
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}
	}

	// patch floatingIP in status
	if lb.Status.ExternalIP == nil || *lb.Status.ExternalIP != fip.FloatingIP {
		r.Log.Info("Update ExternalIP", "lb", lb.Name)
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			ExternalIP: &fip.FloatingIP,
		}); err != nil {
			return false, fmt.Errorf("failed to add FIP to lb status: %w", err)
		}
		requeue = true
	}

	// if FIP is managed by user but has FloatingName as its name
	// we need to add "USER MANAGED" to its name
	// otherwhise it might be picked up by our controller in the future
	// and gets assigned to a certain service
	// edge case: user claims FIP which was managed by YAWOL before
	if lb.Spec.ExistingFloatingIP != nil &&
		lb.Status.FloatingName != nil &&
		fip.Description == *lb.Status.FloatingName {
		name := fip.Description + " (user managed)"
		_, err = fipClient.Update(ctx, fip.ID, floatingips.UpdateOpts{Description: &name})
		if err != nil {
			return false, fmt.Errorf("failed to mark FIP as user managed: %w", err)
		}
	}

	return requeue, nil
}

func (r *Reconciler) assignOrCreateFIP(
	ctx context.Context,
	fipClient openstack.FipClient,
	lb *yawolv1beta1.LoadBalancer,
) error {
	var fip *floatingips.FloatingIP
	var err error

	// use existing floating ip
	if lb.Spec.ExistingFloatingIP != nil {
		r.Log.Info("Use ExistingFloatingIP", "lb", lb.Name)
		if fip, err = openstackhelper.GetFIPByIP(ctx, fipClient, *lb.Spec.ExistingFloatingIP); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("configured fip not found in openstack", "fip", *lb.Spec.ExistingFloatingIP)
				return kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			default:
				r.Log.Error(err, "retrieving FIP by IP failed")
				return kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}
		if fip.ID != "" {
			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
				FloatingID: &fip.ID,
			}); err != nil {
				return err
			}
		}

		return nil
	}

	// try to find FIP by name
	fip, _ = openstackhelper.GetFIPByName(ctx, fipClient, *lb.Status.FloatingName)
	if fip != nil {
		r.Log.Info("Found FloatingIP by Name", "lb", lb.Name)
		return helper.PatchLBStatus(
			ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{FloatingID: &fip.ID},
		)
	}

	// create fip
	r.Log.Info("Create FloatingIP", "lb", lb.Name)
	if fip, err = openstackhelper.CreateFIP(ctx, fipClient, lb); err != nil {
		return kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
	}
	// double check so status won't be corrupted
	if fip.ID == "" {
		return helper.ErrFIPIDEmpty
	}

	return helper.PatchLBStatus(
		ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{FloatingID: &fip.ID},
	)
}

func (r *Reconciler) reconcilePort( //nolint: gocyclo // TODO reduce complexity in future
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
	r.Log.Info("Reconcile Port", "lb", lb.Name)
	var requeue bool
	var portClient openstack.PortClient
	var err error

	portClient, err = osClient.PortClient(ctx)

	if err != nil {
		return false, err
	}

	// Patch Floating Name, so we can reference it later
	if lb.Status.PortName == nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			PortName: ptr.To(req.NamespacedName.String()),
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	var port *ports.Port

	// try to get port my name to use it if possible
	if lb.Status.PortID == nil {
		port, err = openstackhelper.GetPortByName(ctx, portClient, *lb.Status.PortName)
		if err != nil {
			return false, err
		}
		if port != nil {
			r.Log.Info("found port with name", "id", port.ID, "lb", req.NamespacedName, "portName", *lb.Status.PortName)
			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{PortID: &port.ID}); err != nil {
				return false, err
			}
			requeue = true
		}
	}

	// Create Port
	if lb.Status.PortID == nil {
		r.Log.Info("Create Port", "lb", lb.Name)

		// TODO cleanup after removing deprecated fields
		var networkID string
		if lb.Spec.Infrastructure.NetworkID != "" { //nolint: staticcheck // needed to be backwards compatible
			networkID = lb.Spec.Infrastructure.NetworkID //nolint: staticcheck // needed to be backwards compatible
		}
		if lb.Spec.Infrastructure.DefaultNetwork.NetworkID != "" {
			networkID = lb.Spec.Infrastructure.DefaultNetwork.NetworkID
		}

		var subnetID string
		if lb.Spec.Infrastructure.DefaultNetwork.SubnetID != nil {
			subnetID = *lb.Spec.Infrastructure.DefaultNetwork.SubnetID
		}

		port, err = openstackhelper.CreatePort(
			ctx,
			portClient,
			*lb.Status.PortName,
			networkID,
			subnetID,
		)
		if err != nil {
			r.Log.Info("unexpected error occurred claiming a port", "lb", req.NamespacedName)
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}

		// double check so status won't be corrupted
		if port.ID == "" {
			return false, helper.ErrPortIDEmpty
		}

		r.Log.Info("successfully created port", "id", port.ID, "lb", req.NamespacedName)

		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			PortID: &port.ID,
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	// Check if port still exists properly
	if lb.Status.PortID != nil {
		if port, err = openstackhelper.GetPortByID(ctx, portClient, *lb.Status.PortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("port not found in openstack", "portID", *lb.Status.PortID)
				if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "portID"); err != nil {
					return false, err
				}
				return true, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			default:
				r.Log.Info("unexpected error occurred")
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		// check if security groups are attached to port
		if lb.Status.SecurityGroupID != nil &&
			(len(port.SecurityGroups) != 1 || port.SecurityGroups[0] != *lb.Status.SecurityGroupID) {
			if _, err := portClient.Update(ctx, port.ID, ports.UpdateOpts{
				SecurityGroups: &[]string{*lb.Status.SecurityGroupID},
			}); err != nil {
				r.Log.Error(err, "could not update port.securitygroups", "lb", lb)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
			requeue = true
		}

		var changed bool
		changed, err = openstackhelper.BindSecGroupToPortIfNeeded(
			ctx,
			portClient,
			lb.Status.SecurityGroupID,
			port,
		)
		if err != nil {
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}

		requeue = requeue || changed
	}

	if port == nil {
		return true, nil
	}

	// Patch PortIP to status
	if len(port.FixedIPs) >= 1 &&
		(lb.Status.PortIP == nil || *lb.Status.PortIP != port.FixedIPs[0].IPAddress) {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			PortIP: &port.FixedIPs[0].IPAddress,
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	// If internal LB, use first port ip as external ip
	if lb.Spec.Options.InternalLB && len(port.FixedIPs) >= 1 &&
		(lb.Status.ExternalIP == nil || *lb.Status.ExternalIP != port.FixedIPs[0].IPAddress) {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			ExternalIP: &port.FixedIPs[0].IPAddress,
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	return requeue, nil
}

func (r *Reconciler) reconcileSecGroup(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
	r.Log.Info("Reconcile SecGroup", "lb", lb.Name)

	var requeue bool
	var err error

	var groupClient openstack.GroupClient
	groupClient, err = osClient.GroupClient(ctx)
	if err != nil {
		return false, err
	}

	var ruleClient openstack.RuleClient
	ruleClient, err = osClient.RuleClient(ctx)
	if err != nil {
		return false, err
	}

	// Patch SecurityGroup Name, so we can reference it later
	if lb.Status.SecurityGroupName == nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			SecurityGroupName: ptr.To(req.NamespacedName.String()),
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	// Reuse SecGroup if SecGroup found by name
	var secGroup *groups.SecGroup

	if lb.Status.SecurityGroupID == nil {
		secGroup, err = openstackhelper.GetSecGroupByName(ctx, groupClient, *lb.Status.SecurityGroupName)
		if err != nil {
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}
		if secGroup != nil {
			r.Log.Info("Found SecGroup by name", "lb", lb.Name)
			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{SecurityGroupID: &secGroup.ID}); err != nil {
				return false, err
			}
			requeue = true
		}
	}

	// Create SecGroup
	if lb.Status.SecurityGroupID == nil {
		r.Log.Info("Create SecGroup", "lb", lb.Name)
		secGroup, err = openstackhelper.CreateSecGroup(ctx, groupClient, req.NamespacedName.String())
		if err != nil {
			r.Log.Info("unexpected error occurred claiming a fip", "lb", req.NamespacedName)
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}
		// double check so status won't be corrupted
		if secGroup.ID == "" {
			r.Log.Info(helper.ErrSecGroupIDEmpty.Error(), "lb", req.NamespacedName)
			return false, helper.ErrSecGroupIDEmpty
		}
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			SecurityGroupID: &secGroup.ID,
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	// Fetch SecGroup for ID
	if lb.Status.SecurityGroupID != nil {
		if secGroup, err = openstackhelper.GetSecGroupByID(ctx, groupClient, *lb.Status.SecurityGroupID); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("SecurityGroupID not found in openstack", "SecurityGroupID", *lb.Status.SecurityGroupID)
				if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "securityGroupID"); err != nil {
					return false, err
				}
				return true, nil
			default:
				r.Log.Info("unexpected error occurred")
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}
	}

	if secGroup == nil {
		r.Log.Info(helper.ErrSecGroupNil.Error())
		return false, helper.ErrSecGroupNil
	}

	r.Log.Info("Reconcile SecGroupRules", "lb", lb.Name)
	desiredSecGroups := helper.GetDesiredSecGroupRulesForLoadBalancer(r.RecorderLB, lb, secGroup.ID)

	err = openstackhelper.DeleteUnusedSecGroupRulesFromSecGroup(ctx, ruleClient, secGroup, desiredSecGroups)
	if err != nil {
		return true, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
	}

	err = openstackhelper.CreateNonExistingSecGroupRules(ctx, ruleClient, req.NamespacedName.String(), secGroup, desiredSecGroups)
	if err != nil {
		return true, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
	}

	return requeue, nil
}

func (r *Reconciler) reconcileServerGroup(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
	r.Log.Info("Reconcile server group", "lb", lb.Name)
	var requeue bool
	var serverGroupClient openstack.ServerGroupClient
	var err error

	serverGroupClient, err = osClient.ServerGroupClient(ctx)
	if err != nil {
		return false, err
	}

	// Patch server group name, so we can reference it later
	if lb.Status.ServerGroupName == nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			ServerGroupName: ptr.To(req.NamespacedName.String()),
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	var serverGroup *servergroups.ServerGroup

	// try to get server group my name to use it if possible
	if lb.Status.ServerGroupID == nil {
		serverGroup, err = openstackhelper.GetServerGroupByName(ctx, serverGroupClient, *lb.Status.ServerGroupName)
		if err != nil {
			return false, err
		}
		if serverGroup != nil {
			r.Log.Info("found server group with name", "id", serverGroup.ID, "lb", req.NamespacedName, "ServerGroupName", *lb.Status.ServerGroupName)
			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{ServerGroupID: &serverGroup.ID}); err != nil {
				return false, err
			}
			requeue = true
		}
	}

	// Create server group
	if lb.Status.ServerGroupID == nil {
		r.Log.Info("Create ServerGroup", "lb", lb.Name)
		serverGroup, err = openstackhelper.CreateServerGroup(
			ctx, serverGroupClient,
			*lb.Status.ServerGroupName,
			lb.Spec.Options.ServerGroupPolicy)
		if err != nil {
			r.Log.Info("unexpected error occurred creating a server group", "lb", req.NamespacedName)
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}

		// double check so status won't be corrupted
		if serverGroup.ID == "" {
			return false, helper.ErrServerGroupIDEmpty
		}

		r.Log.Info("successfully created server group", "id", serverGroup.ID, "lb", req.NamespacedName)

		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			ServerGroupID: &serverGroup.ID,
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	// Check if server group still exists properly
	if lb.Status.ServerGroupID != nil {
		if serverGroup, err = openstackhelper.GetServerGroupByID(ctx, serverGroupClient, *lb.Status.ServerGroupID); err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("server group not found in openstack", "serverGroupID", *lb.Status.ServerGroupID)
				if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "serverGroupID"); err != nil {
					return false, err
				}
				return true, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			default:
				r.Log.Info("unexpected error occurred")
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}
		if len(serverGroup.Policies) != 1 || serverGroup.Policies[0] != lb.Spec.Options.ServerGroupPolicy {
			r.Log.Info("server group not correct affinity policy delete", "serverGroupID", *lb.Status.ServerGroupID)
			if err = openstackhelper.DeleteServerGroup(ctx, serverGroupClient, serverGroup.ID); err != nil {
				r.Log.Info("unexpected error occurred deleting a server group", "lb", req.NamespacedName)
				return false, err
			}
			requeue = true
		}
	}
	return requeue, nil
}

func (r *Reconciler) reconcileFIPAssociate(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
	if lb.Spec.Options.InternalLB {
		return false, nil
	}

	r.Log.Info("Reconcile FIPAssociate", "lb", lb.Name)

	var err error

	var fipClient openstack.FipClient
	fipClient, err = osClient.FipClient(ctx)
	if err != nil {
		return false, err
	}

	if lb.Status.PortID == nil || lb.Status.FloatingID == nil {
		r.Log.WithName("reconcileFIPAssociate").Info("either portID or floatingID is null", "lb", req)
		return true, nil
	}

	var fip *floatingips.FloatingIP

	if fip, err = openstackhelper.GetFIPByID(ctx, fipClient, *lb.Status.FloatingID); err != nil {
		return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
	}

	// fip is already on correct port and fip status is Active - skip binding
	if fip.PortID == *lb.Status.PortID && fip.Status == "ACTIVE" {
		return false, nil
	}

	r.Log.Info("Bind FloatingIP to Port", "lb", lb.Name)

	if err := openstackhelper.BindFIPToPort(ctx, fipClient, *lb.Status.FloatingID, lb.Status.PortID); err != nil {
		r.Log.WithName("reconcileFIPAssociate").
			Info("failed to associate fip with port",
				"lb", req, "fip",
				*lb.Status.FloatingID, "port", *lb.Status.PortID)
		return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
	}

	return false, nil
}

func (r *Reconciler) reconcileLoadBalancerSet(
	ctx context.Context,
	log logr.Logger,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	emptyResult := ctrl.Result{}
	// Make sure LoadBalancer has revision number
	currentRevision, err := helper.ReadCurrentRevisionFromLB(lb)
	if err != nil {
		return emptyResult, err
	}
	log = log.WithValues("revision", currentRevision)

	if currentRevision == 0 {
		return emptyResult, helper.PatchLoadBalancerRevision(ctx, r.Client, lb, 1)
	}

	// Get the labels LBS will receive on creation from this lb
	lbsLabels := helper.GetLoadBalancerSetLabelsFromLoadBalancer(lb)

	loadBalancerSetList := &yawolv1beta1.LoadBalancerSetList{}
	if err := r.Client.List(ctx, loadBalancerSetList, client.MatchingLabels(lbsLabels)); err != nil {
		return emptyResult, err
	}

	// Get Hash for current LoadBalancerMachineSpec
	var hash string
	hash, err = helper.GetHashForLoadBalancerMachineSet(lb)
	if err != nil {
		return emptyResult, err
	}
	log = log.WithValues("hash", hash)

	// Get LoadBalancerSet for current hash
	var currentSet *yawolv1beta1.LoadBalancerSet
	if currentSet, err = helper.GetLoadBalancerSetForHash(loadBalancerSetList, hash); err != nil {
		return emptyResult, err
	}

	// create new LBS if hash changed or LB was newly created
	if currentSet == nil {
		nextRevision, err := helper.GetNextRevisionForLoadBalancer(loadBalancerSetList)
		if err != nil {
			return emptyResult, err
		}

		if lb.Status.PortID == nil {
			return emptyResult, helper.ErrLBPortNotSet
		}

		if err := helper.PatchLoadBalancerRevision(ctx, r.Client, lb, nextRevision); err != nil {
			return emptyResult, err
		}

		currentSet = helper.NewLoadBalancerSetForLoadBalancer(lb, hash, nextRevision)
		log = log.WithValues("loadBalancerSet", currentSet.Name)
		log.Info("Creating new LoadBalancerSet")

		if err := r.Client.Create(ctx, currentSet); err != nil {
			return emptyResult, err
		}
	}
	log = log.WithValues("loadBalancerSet", currentSet.Name)

	// update LB status according to observed LBS status
	patch := client.MergeFrom(lb.DeepCopy())
	statusReplicas, statusReadyReplicas := helper.StatusReplicasFromSetList(loadBalancerSetList)
	lb.Status.Replicas, lb.Status.ReadyReplicas = &statusReplicas, &statusReadyReplicas
	if err := r.Client.Status().Patch(ctx, lb, patch); err != nil {
		return emptyResult, err
	}

	// update LoadBalancerSet replicas if required
	desiredReplicas := lb.Spec.Replicas
	if currentSet.Spec.Replicas != desiredReplicas {
		log.Info("Setting replicas of current LoadBalancerSet", "replicas", desiredReplicas)
		if err := helper.PatchLoadBalancerSetReplicas(ctx, r.Client, currentSet, desiredReplicas); err != nil {
			return emptyResult, err
		}
	}

	var (
		currentReplicas      = ptr.Deref(currentSet.Status.Replicas, 0)
		currentReadyReplicas = ptr.Deref(currentSet.Status.ReadyReplicas, 0)
	)
	log = log.WithValues("replicas", currentReplicas, "readyReplicas", currentReadyReplicas)

	// check if all replicas from current LBS are ready and requeue if not ready
	if currentReplicas != desiredReplicas || currentReadyReplicas != desiredReplicas {
		log.V(1).Info("Current LoadBalancerSet is not ready yet")
		return emptyResult, nil
	}

	log.Info("Current LoadBalancerSet is ready")

	// check if in current loadbalancerset is a keepalived master
	if ok, requeueAfter := helper.LBSetHasKeepalivedMaster(currentSet); !ok {
		// the helper only returns true if the LBSet has the master for some
		// time, so we want to requeue more frequently to catch that.
		if requeueAfter > 5*time.Minute {
			requeueAfter = 5 * time.Minute
		}
		log.V(1).Info("Current LoadBalancerSet has no keepalived master yet", "forceCheckAfter", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter, Requeue: true}, nil
	}

	log.Info("Current LoadBalancerSet has keepalived master")

	// scale down all other existing LoadBalancerSets for the LoadBalancer after upscale to ensure no downtime
	log.Info("Scale down old LoadBalancerSets")
	if err := helper.ScaleDownOldLoadBalancerSets(ctx, r.Client, loadBalancerSetList, currentSet.Name); err != nil {
		return emptyResult, nil
	}

	return emptyResult, nil
}

func (r *Reconciler) deletionRoutine(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	// Clean up all LoadBalancerSets
	list, err := helper.GetLoadBalancerSetsForLoadBalancer(ctx, r.Client, lb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get lb sets for lb: %w", err)
	}
	for i := range list.Items {
		if list.Items[i].DeletionTimestamp == nil {
			if err := r.Client.Delete(ctx, &list.Items[i]); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete item %v: %w", list.Items[i].Name, err)
			}
		}
	}

	if len(list.Items) != 0 {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	var requeue bool

	requeue, err = r.deleteServerGroups(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete server groups: %w", err)
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	requeue, err = r.deleteFips(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete FIPs: %w", err)
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	requeue, err = r.deletePorts(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete ports: %w", err)
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	requeue, err = r.deleteSecGroups(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete sec groups: %w", err)
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	helper.RemoveLoadBalancerMetrics(
		*lb,
		r.Metrics,
	)

	if err := kubernetes.RemoveFinalizerIfNeeded(ctx, r.Client, lb, ServiceFinalizer); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// Deletes floating ips related to the LoadBalancer object
// 1. Retrieves FIP by ID in lb.Status.FloatingID
// 1.1 if found => delete FIP
// 2. Retrieves FIP by Name in lb.Status.FloatingName
// 2.1 if found => delete FIP
// Returns any error except for 404 errors from gophercloud
func (r *Reconciler) deleteFips(
	ctx context.Context,
	osClient openstack.Client,
	lb *yawolv1beta1.LoadBalancer,
) (bool, error) {
	var err error

	var fipClient openstack.FipClient
	fipClient, err = osClient.FipClient(ctx)
	if err != nil {
		return false, err
	}

	var requeue = false

	// skip deletion and release status when annotated
	if keep, err := strconv.ParseBool(lb.GetAnnotations()[yawolv1beta1.LoadBalancerKeepFloatingIP]); err == nil && keep {
		if lb.Status.FloatingID == nil &&
			lb.Status.FloatingName == nil {
			return false, nil
		}
		r.Log.Info("FIP released.", "lb", lb.Namespace+"/"+lb.Name)
		err = helper.RemoveFromLBStatus(ctx, r.Client.Status(), lb, "floatingID")
		if err != nil {
			return false, err
		}
		err = helper.RemoveFromLBStatus(ctx, r.Client.Status(), lb, "floatingName")
		if err != nil {
			return false, err
		}
		return true, nil
	}

	// Remove only from status if lb.Spec.ExistingFloatingIP is set
	if lb.Spec.ExistingFloatingIP != nil {
		if lb.Status.FloatingID == nil &&
			lb.Status.FloatingName == nil {
			return false, nil
		}
		r.Log.Info("existing floating IP used, skip FIP deletion", "lb", lb.Namespace+"/"+lb.Name)
		err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingID")
		if err != nil {
			return false, err
		}
		err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingName")
		if err != nil {
			return false, err
		}
		// next run will return false because status is set to nil
		return true, nil
	}

	if lb.Status.FloatingID != nil {
		fip, err := openstackhelper.GetFIPByID(ctx, fipClient, *lb.Status.FloatingID)
		if err != nil {
			// if error is our custom not found error
			if err == helper.ErrFIPNotFound {
				r.Log.Info("fip not found, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				// requeue true to delete floatingName in the next run
				return true, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingID")
			}

			// if error is openstack error
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("error getting fip, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				// requeue true to delete fips by floatingName in the next run
				return true, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingID")
			default:
				r.Log.Info("an unexpected error occurred retrieving fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		if fip != nil {
			if err := openstackhelper.DeleteFIP(ctx, fipClient, fip.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		// requeue so next run will delete the status
		requeue = true
	}

	// clean orphan fips
	if lb.Status.FloatingName != nil {
		fipName := *lb.Status.FloatingName
		var fipList []floatingips.FloatingIP
		fipList, err = fipClient.List(ctx, floatingips.ListOpts{
			Description: fipName,
		})
		if err != nil {
			return false, err
		}

		if len(fipList) == 0 {
			r.Log.Info("no fips found by name, fips are already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipName", *lb.Status.FloatingName)
			// everything is cleaned, no requeue
			return false, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingName")
		}

		for i := range fipList {
			fip := &fipList[i]
			// double check
			if fip.ID == "" || fip.Description != fipName {
				continue
			}

			if err := openstackhelper.DeleteFIP(ctx, fipClient, fip.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", fip.ID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		// requeue so next run will delete the status
		requeue = true
	}

	return requeue, nil
}

// Deletes ports related to the LoadBalancer object
// 1. Retrieves port by ID in lb.Status.PortID
// 1.1 if found => delete port
// 2. Retrieves port by Name in lb.Status.PortName
// 2.1 if found => delete port
// Returns any error except for 404 errors from gophercloud
func (r *Reconciler) deletePorts(
	ctx context.Context,
	osClient openstack.Client,
	lb *yawolv1beta1.LoadBalancer,
) (bool, error) {
	var err error

	portClient, err := osClient.PortClient(ctx)
	if err != nil {
		return false, err
	}

	// skip deletion and release status when annotated
	if keep, err := strconv.ParseBool(lb.GetAnnotations()[yawolv1beta1.LoadBalancerKeepPort]); err == nil && keep {
		if lb.Status.PortID == nil {
			return false, nil
		}
		r.Log.Info("Port released", "lb", lb.Namespace+"/"+lb.Name)
		err = helper.RemoveFromLBStatus(ctx, r.Client.Status(), lb, "portID")
		if err != nil {
			return false, err
		}
		return true, nil
	}

	var requeue bool

	if lb.Status.PortID != nil {
		port, err := openstackhelper.GetPortByID(ctx, portClient, *lb.Status.PortID)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("port has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				// requeue true to clean orphan ports in the next run
				_ = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "portIP")
				return true, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "portID")
			default:
				r.Log.Info("an unexpected error occurred retrieving port", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				return false, err
			}
		}
		if port != nil {
			if err := openstackhelper.DeletePort(ctx, portClient, port.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting port", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}
		// requeue so next run will delete the status
		requeue = true
	}

	// clean up orphan ports
	if lb.Status.PortName != nil {
		portName := *lb.Status.PortName
		var portList []ports.Port
		portList, err = portClient.List(ctx, ports.ListOpts{Name: portName})
		if err != nil {
			return false, err
		}

		if len(portList) == 0 {
			r.Log.Info("no ports found by name, ports are already deleted", "lb", lb.Namespace+"/"+lb.Name, "portName", portName)
			// everything is cleaned, no requeue
			return false, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "portName")
		}

		for i := range portList {
			port := &portList[i]
			// double check
			if port.ID == "" || port.Name != portName {
				continue
			}

			if err := openstackhelper.DeletePort(ctx, portClient, port.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting port", "lb", lb.Namespace+"/"+lb.Name, "portID", port.ID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		// requeue so next run will delete the status
		requeue = true
	}

	return requeue, nil
}

// Deletes SecGroups related to the LoadBalancer objects and disassociate it from other ports
// 1. Removes sec group from all listable ports
// 2. Retrieves sec group by ID in lb.Status.SecGroupID
// 2.1 if found => delete sec group
// 3. Retrieves sec group by Name in lb.Status.SecGroupName
// 3.1 if found => delete sec group
func (r *Reconciler) deleteSecGroups(
	ctx context.Context,
	osClient openstack.Client,
	lb *yawolv1beta1.LoadBalancer,
) (bool, error) {
	var err error

	portClient, err := osClient.PortClient(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to create port client: %w", err)
	}

	groupClient, err := osClient.GroupClient(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to create group client: %w", err)
	}

	err = r.findAndDeleteSecGroupUsages(ctx, portClient, lb)
	if err != nil {
		return false, fmt.Errorf("failed to delete sec group usages: %w", err)
	}
	// skip deletion and release status when annotated
	if keep, err := strconv.ParseBool(lb.GetAnnotations()[yawolv1beta1.LoadBalancerKeepSecurityGroup]); err == nil && keep {
		if lb.Status.SecurityGroupID == nil {
			return false, nil
		}
		r.Log.Info("security group was released", "lb", lb.Namespace+"/"+lb.Name)
		err = helper.RemoveFromLBStatus(ctx, r.Client.Status(), lb, "security_group_id")
		if err != nil {
			return true, fmt.Errorf("failed to remove from lb status: %w", err)
		}
		return false, nil
	}

	var requeue bool
	if lb.Status.SecurityGroupID != nil {
		secGroup, err := openstackhelper.GetSecGroupByID(ctx, groupClient, *lb.Status.SecurityGroupID)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("secGroup has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				// requeue to clean orphan secgroups
				err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "securityGroupID")
				if err != nil {
					return true, fmt.Errorf("failed to remove securityGroupID from lb status: %w", err)
				}
				return true, nil
			default:
				r.Log.Info("an unexpected error occurred retrieving secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				return false, fmt.Errorf("failed to get sec group by ID: %w", err)
			}
		}
		if secGroup != nil {
			if err := openstackhelper.DeleteSecGroup(ctx, groupClient, secGroup.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		// requeue to delete status in next run
		requeue = true
	}

	// clean up orphan secgroups
	if lb.Status.SecurityGroupName != nil {
		secGroupName := *lb.Status.SecurityGroupName
		var secGroupList []groups.SecGroup
		secGroupList, err := groupClient.List(ctx, groups.ListOpts{Name: secGroupName})
		if err != nil {
			return false, fmt.Errorf("failed to list sec groups: %w", err)
		}

		if len(secGroupList) == 0 {
			r.Log.Info(
				"no secGroups found by name, secGroups are already deleted", "lb",
				lb.Namespace+"/"+lb.Name, "secGroup", secGroupName,
			)
			// no requeue, everything is cleaned
			err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "securityGroupName")
			if err != nil {
				return false, fmt.Errorf("failed to remove securityGroupName from lb status: %w", err)
			}
			return false, nil
		}

		for i := range secGroupList {
			secGroup := &secGroupList[i]
			if secGroup.ID == "" || secGroup.Name != secGroupName {
				// double check
				continue
			}

			if err := openstackhelper.DeleteSecGroup(ctx, groupClient, secGroup.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", secGroup.ID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}
		// requeue so next run will delete the status
		requeue = true
	}

	return requeue, nil
}

// Deletes ServerGroups related to the LoadBalancer servers
// 1. Retrieves servergroup by ID in lb.Status.ServerGroupID
// 1.1 if found => delete servergroup
// 2. Retrieves servergroup by Name in lb.Status.ServerGroupName
// 2.1 if found => delete all servergroups
func (r *Reconciler) deleteServerGroups(
	ctx context.Context,
	osClient openstack.Client,
	lb *yawolv1beta1.LoadBalancer,
) (bool, error) {
	var err error

	serverGroupClient, err := osClient.ServerGroupClient(ctx)
	if err != nil {
		return false, err
	}

	var requeue bool
	if lb.Status.ServerGroupID != nil {
		serverGroup, err := openstackhelper.GetServerGroupByID(ctx, serverGroupClient, *lb.Status.ServerGroupID)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("server group has already been deleted",
					"lb", lb.Namespace+"/"+lb.Name, "serverGroupID", *lb.Status.ServerGroupID)
				// requeue to clean orphan server groups
				return true, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "serverGroupID")
			default:
				r.Log.Info("an unexpected error occurred retrieving secGroup",
					"lb", lb.Namespace+"/"+lb.Name, "serverGroupID", *lb.Status.ServerGroupID)
				return false, err
			}
		}
		if serverGroup != nil {
			if err := openstackhelper.DeleteServerGroup(ctx, serverGroupClient, serverGroup.ID); err != nil {
				r.Log.Info("an unexpected error occurred deleting server group",
					"lb", lb.Namespace+"/"+lb.Name, "serverGroupID", *lb.Status.ServerGroupID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		// requeue to delete status in next run
		requeue = true
	}

	// clean up orphan serverGroup
	if lb.Status.ServerGroupName != nil {
		serverGroup, err := openstackhelper.GetServerGroupByName(ctx, serverGroupClient, *lb.Status.ServerGroupName)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrUnexpectedResponseCode, gophercloud.ErrResourceNotFound:
				r.Log.Info("server group has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "serverGroup", *lb.Status.ServerGroupName)
				// requeue to clean orphan secgroups
				return true, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "serverGroupName")
			default:
				r.Log.Info("an unexpected error occurred retrieving secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				return false, err
			}
		}

		if serverGroup == nil {
			r.Log.Info("server group has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "serverGroup", *lb.Status.ServerGroupName)
			// requeue to clean orphan secgroups
			return true, helper.RemoveFromLBStatus(ctx, r.Status(), lb, "serverGroupName")
		}

		if err := openstackhelper.DeleteServerGroup(ctx, serverGroupClient, serverGroup.ID); err != nil {
			r.Log.Info("an unexpected error occurred deleting serverGroup", "lb", lb.Namespace+"/"+lb.Name, "serverGroupID", serverGroup.ID)
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}

		// requeue so next run will delete the status
		requeue = true
	}
	return requeue, nil
}

func (r *Reconciler) findAndDeleteSecGroupUsages(
	ctx context.Context,
	portClient openstack.PortClient,
	lb *yawolv1beta1.LoadBalancer,
) error {
	if lb.Status.SecurityGroupID == nil {
		return nil
	}

	var listedPorts []ports.Port
	var err error
	listedPorts, err = openstackhelper.GetAllPorts(ctx, portClient)
	if err != nil {
		return fmt.Errorf("failed to get all ports: %w", err)
	}

	for i := range listedPorts {
		err = openstackhelper.RemoveSecGroupFromPortIfNeeded(ctx, portClient, &listedPorts[i], *lb.Status.SecurityGroupID)
		if err != nil {
			err = kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			if err != nil {
				return fmt.Errorf("failed to remove sec group from port %v: %w", listedPorts[i].ID, err)
			}
			return nil
		}
	}

	return nil
}
