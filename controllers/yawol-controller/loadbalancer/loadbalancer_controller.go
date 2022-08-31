package loadbalancer

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"
	"github.com/stackitcloud/yawol/internal/openstack"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	openstackhelper "github.com/stackitcloud/yawol/internal/helper/openstack"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	DefaultRequeueTime = 10 * time.Millisecond
	RevisionAnnotation = "loadbalancer.yawol.stackit.cloud/revision"
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
	getOsClientForIni func(iniData []byte) (openstack.Client, error)
	WorkerCount       int
	OpenstackTimeout  time.Duration
}

// Reconcile function for LoadBalancer object
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancer", req.NamespacedName)
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
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// migrate deprecations TODO: remove later
	if res, err := r.migrateDeprecations(ctx, &lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	// update metrics
	helper.ParseLoadBalancerMetrics(
		lb,
		r.Metrics,
	)

	// get OpenStack Client for LoadBalancer
	osClient, err := openstackhelper.GetOpenStackClientForAuthRef(ctx, r.Client, lb.Spec.Infrastructure.AuthSecretRef, r.getOsClientForIni)
	if err != nil {
		return ctrl.Result{}, err
	}

	var res ctrl.Result

	// handle deletion
	if lb.GetDeletionTimestamp() != nil {
		if res, err := r.deletionRoutine(ctx, &lb, osClient); err != nil || res.Requeue || res.RequeueAfter != 0 {
			return res, err
		}
		return ctrl.Result{}, nil
	}

	// adds finalizer if not set
	if err := kubernetes.AddFinalizerIfNeeded(ctx, r.Client, &lb, ServiceFinalizer); err != nil {
		return ctrl.Result{}, err
	}

	// run openstack reconcile if needed
	if res, err = r.reconcileOpenStackIfNeeded(ctx, &lb, req, osClient); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	// lbs reconcile is not affected by lastOpenstackReconcile
	if res, err = r.reconcileLoadBalancerSet(ctx, &lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	return ctrl.Result{}, nil
}

// migrateDeprecations moves the deprecated fields to the new object
// TODO: remove in future
func (r *Reconciler) migrateDeprecations(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	// internalLB
	if lb.Spec.InternalLB {
		// add to options
		patch := []byte(`{"spec":{"options":{"internalLB":` + strconv.FormatBool(lb.Spec.InternalLB) + `}}}`)
		if err := r.Client.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch)); err != nil {
			return ctrl.Result{}, err
		}
		// remove old field
		patch = []byte(`[{"op":"remove", "path":"/spec/internalLB"}]`)
		if err := r.Client.Patch(ctx, lb, client.RawPatch(types.JSONPatchType, patch)); err != nil {
			return ctrl.Result{}, err
		}
	}
	// loadBalancerSourceRanges
	if lb.Spec.LoadBalancerSourceRanges != nil && len(lb.Spec.LoadBalancerSourceRanges) > 0 {
		// add to options
		data, err := json.Marshal(lb.Spec.LoadBalancerSourceRanges)
		if err != nil {
			return ctrl.Result{}, err
		}
		patch := []byte(`{"spec":{"options":{"loadBalancerSourceRanges":` + string(data) + `}}}`)
		if err := r.Client.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch)); err != nil {
			return ctrl.Result{}, err
		}
		// remove old field
		patch = []byte(`[{"op":"remove", "path":"/spec/loadBalancerSourceRanges"}]`)
		if err := r.Client.Patch(ctx, lb, client.RawPatch(types.JSONPatchType, patch)); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.getOsClientForIni == nil {
		r.getOsClientForIni = func(iniData []byte) (openstack.Client, error) {
			osClient := openstack.OSClient{}
			err := osClient.Configure(iniData, r.OpenstackTimeout, r.Metrics.OpenstackMetrics)
			if err != nil {
				return nil, err
			}
			return &osClient, nil
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancer{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
		}).
		Complete(r)
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

	var requeue, overallRequeue bool
	var err error

	requeue, err = r.reconcileSecGroup(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, err
	}
	overallRequeue = overallRequeue || requeue

	requeue, err = r.reconcileFIP(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, err
	}
	overallRequeue = overallRequeue || requeue

	requeue, err = r.reconcilePort(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, err
	}
	overallRequeue = overallRequeue || requeue

	requeue, err = r.reconcileFIPAssociate(ctx, req, lb, osClient)
	if err != nil {
		return ctrl.Result{}, err
	}
	overallRequeue = overallRequeue || requeue

	if overallRequeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
		LastOpenstackReconcile: &metaV1.Time{Time: time.Now()},
	}); err != nil {
		return ctrl.Result{}, err
	}

	err = r.updateOpenstackReconcileHash(ctx, lb)
	if err != nil {
		return ctrl.Result{}, err
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
		return err
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
		return r.deleteFips(ctx, osClient, lb)
	}

	var err error

	// get openstack fip client
	var fipClient openstack.FipClient
	fipClient, err = osClient.FipClient(ctx)
	if err != nil {
		return false, err
	}

	var requeue bool

	// Patch Floating Name, so we can reference it later
	if lb.Status.FloatingName == nil {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			FloatingName: pointer.String(req.NamespacedName.String()),
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	var fip *floatingips.FloatingIP

	// get floatingIP by name if ID is not set and patch ID in loadbalancer object
	if lb.Status.FloatingID == nil {
		fip, err = openstackhelper.GetFIPByName(ctx, fipClient, *lb.Status.FloatingName)
		if err != nil {
			return false, err
		}
		if fip != nil {
			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{FloatingID: &fip.ID}); err != nil {
				return false, err
			}
			requeue = true
		}
	}

	if lb.Status.FloatingID == nil {
		// Use existing Floating IP if specified
		if lb.Spec.ExistingFloatingIP != nil {
			if fip, err = openstackhelper.GetFIPByIP(ctx, fipClient, *lb.Spec.ExistingFloatingIP); err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("configured fip not found in openstack", "fip", *lb.Spec.ExistingFloatingIP)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				default:
					r.Log.Info("unexpected error occurred")
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
			}
			if fip.ID != "" {
				if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
					FloatingID: &fip.ID,
				}); err != nil {
					return false, err
				}
			}
		} else {
			// create fip
			if fip, err = openstackhelper.CreateFIP(ctx, fipClient, lb); err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault400:
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				case gophercloud.ErrDefault409:
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				default:
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
			}
			// double check so status won't be corrupted
			if fip.ID == "" {
				return false, helper.ErrFIPIDEmpty
			}

			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
				FloatingID: &fip.ID,
			}); err != nil {
				return false, err
			}
			requeue = true
		}
	}

	// Get FIP
	if fip, err = openstackhelper.GetFIPByID(ctx, fipClient, *lb.Status.FloatingID); err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault404:
			r.Log.Info("fip not found in openstack", "fip", *lb.Status.FloatingID)
			// fip not found by ID, remove it from status and trigger reconcile
			if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingID"); err != nil {
				return false, err
			}
			return true, err
		default:
			r.Log.Info("unexpected error occurred")
			return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}
	}

	// patch floatingIP in status
	if lb.Status.ExternalIP == nil || *lb.Status.ExternalIP != fip.FloatingIP {
		if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			ExternalIP: &fip.FloatingIP,
		}); err != nil {
			return false, err
		}
		requeue = true
	}

	return requeue, nil
}

func (r *Reconciler) reconcilePort(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
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
			PortName: pointer.String(req.NamespacedName.String()),
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
		port, err = openstackhelper.CreatePort(ctx, portClient, *lb.Status.PortName, lb.Spec.Infrastructure.NetworkID)
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
			case gophercloud.ErrDefault404:
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
			SecurityGroupName: pointer.String(req.NamespacedName.String()),
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
			if err := helper.PatchLBStatus(ctx, r.Status(), lb, yawolv1beta1.LoadBalancerStatus{SecurityGroupID: &secGroup.ID}); err != nil {
				return false, err
			}
			requeue = true
		}
	}

	// Create SecGroup
	if lb.Status.SecurityGroupID == nil {
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
			case gophercloud.ErrDefault404:
				r.Log.Info("SecurityGroupID not found in openstack", "SecurityGroupID", *lb.Status.SecurityGroupID)
				if err := helper.RemoveFromLBStatus(ctx, r.Status(), lb, "security_group_id"); err != nil {
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

func (r *Reconciler) reconcileFIPAssociate(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (bool, error) {
	if lb.Spec.Options.InternalLB {
		return false, nil
	}

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

	// fip is already on coorect port
	if fip.PortID == *lb.Status.PortID {
		return false, nil
	}

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
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	// Make sure LoadBalancer has revision number
	currentRevision, err := helper.ReadCurrentRevisionFromLB(lb)
	if err != nil {
		return ctrl.Result{}, err
	}

	if currentRevision == 0 {
		if err := helper.PatchLoadBalancerRevision(ctx, r.Client, lb, 1); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// Get the labels LBS will receive on creation from this lb
	lbsLabels := helper.GetLoadBalancerSetLabelsFromLoadBalancer(lb)

	// Get Hash for current LoadBalancerMachineSpec
	var hash string
	hash, err = helper.GetHashForLoadBalancerMachineSpecFromLoadBalancer(lb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Get LoadBalancerSet for current hash
	var loadBalancerSet *yawolv1beta1.LoadBalancerSet
	if loadBalancerSet, err = helper.GetLoadBalancerSetForHash(ctx, r.Client, lbsLabels, hash); err != nil {
		return ctrl.Result{}, err
	}

	// set to empty object to prevent nil pointer exception
	if loadBalancerSet == nil {
		loadBalancerSet = &yawolv1beta1.LoadBalancerSet{}
	}

	// create new lbset if hash changed
	if loadBalancerSet.Name == "" {
		var newRevision int
		newRevision, err = helper.GetNextRevisionFromLB(ctx, r.Client, lb)
		if err != nil {
			return ctrl.Result{}, err
		}

		floatingID := ""
		if lb.Status.FloatingID != nil {
			floatingID = *lb.Status.FloatingID
		}

		if err := helper.CreateLoadBalancerSet(ctx, r.Client, lb, &yawolv1beta1.LoadBalancerMachineSpec{
			Infrastructure: lb.Spec.Infrastructure,
			FloatingID:     floatingID,
			LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
				Namespace: lb.Namespace,
				Name:      lb.Name,
			},
		}, hash, newRevision); err != nil {
			return ctrl.Result{}, err
		}

		err = helper.PatchLoadBalancerRevision(ctx, r.Client, lb, newRevision)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// loadBalancerSet is never a newly created one from here on
	// because we early return with a requeue after creation

	// update lb revision to lbset revision
	// in order to show the correct status on the lb object
	lbsRevision, err := helper.ReadRevisionFromLBS(loadBalancerSet)
	if err != nil {
		return ctrl.Result{}, err
	}

	if lbsRevision != currentRevision {
		r.Log.Info("patch lb revision to match lbs revision", "revision", lbsRevision)
		err = helper.PatchLoadBalancerRevision(ctx, r.Client, lb, lbsRevision)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// update replicas
	if loadBalancerSet.Spec.Replicas != lb.Spec.Replicas {
		err := helper.PatchLoadBalancerSetReplicas(ctx, r.Client, loadBalancerSet, lb.Spec.Replicas)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// check if all replicas from current lbset are ready and requeue if current lbset is not fully ready
	ready, err := helper.LoadBalancerSetIsReady(ctx, r.Client, lb, loadBalancerSet)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !ready {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	r.Log.Info("current lbset is ready", "lbs", loadBalancerSet.Name)

	// scale down all other existing lbsets for the LoadBalancer after upscale to ensure no downtime
	downscaled, err := helper.AreAllLoadBalancerSetsForLBButDownscaled(ctx, r.Client, lb, loadBalancerSet.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !downscaled {
		r.Log.Info("scale down all lbsets except of", "lbs", loadBalancerSet.Name)
		return helper.ScaleDownAllLoadBalancerSetsForLBBut(ctx, r.Client, lb, loadBalancerSet.Name)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) deletionRoutine(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	// Clean up all LoadBalancerSets
	list, err := helper.GetLoadBalancerSetsForLoadBalancer(ctx, r.Client, lb)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range list.Items {
		if list.Items[i].DeletionTimestamp == nil {
			if err := r.Client.Delete(ctx, &list.Items[i]); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	if len(list.Items) != 0 {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	var requeue bool
	requeue, err = r.deleteFips(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	requeue, err = r.deletePorts(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	requeue, err = r.deleteSecGroups(ctx, osClient, lb)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	if err := kubernetes.RemoveFinalizerIfNeeded(ctx, r.Client, lb, ServiceFinalizer); err != nil {
		return ctrl.Result{}, err
	}

	helper.RemoveLoadBalancerMetrics(
		*lb,
		r.Metrics,
	)

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

	var fip *floatingips.FloatingIP
	var requeue = false

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
		return true, nil
	}

	if lb.Status.FloatingID != nil {
		fip, err = openstackhelper.GetFIPByID(ctx, fipClient, *lb.Status.FloatingID)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("error getting fip, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingID")
				if err != nil {
					return false, err
				}
			default:
				r.Log.Info("an unexpected error occurred retrieving fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
			}
		}

		if fip != nil {
			err = openstackhelper.DeleteFIP(ctx, fipClient, fip.ID)
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("error deleting fip, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				default:
					r.Log.Info("an unexpected error occurred deleting fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
			}
		}
		// requeue so next run will delete the status
		requeue = true
	}

	if lb.Status.FloatingName != nil { //nolint:dupl // no dupl
		var fipByName *floatingips.FloatingIP

		fipByName, err = openstackhelper.GetFIPByName(ctx, fipClient, *lb.Status.FloatingName)
		if err != nil {
			r.Log.Info("an error occurred extracting floating IPs", "lb", lb.Namespace+"/"+lb.Name)
			return false, err
		}

		if fipByName == nil {
			r.Log.Info("no fips found, fip has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "fipName", *lb.Status.FloatingName)
			err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "floatingName")
			if err != nil {
				return false, err
			}
		}

		if fipByName != nil {
			err = openstackhelper.DeleteFIP(ctx, fipClient, fipByName.ID)
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("error deleting fip, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", fipByName.ID)
				default:
					r.Log.Info("an unexpected error occurred deleting fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", fipByName.ID)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
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

	var port *ports.Port
	var requeue bool

	if lb.Status.PortID != nil {
		port, err = openstackhelper.GetPortByID(ctx, portClient, *lb.Status.PortID)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("port has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "portID")
				if err != nil {
					return false, err
				}
			default:
				r.Log.Info("an unexpected error occurred retrieving port", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				return false, err
			}
		}
		if port != nil {
			err = openstackhelper.DeletePort(ctx, portClient, port.ID)
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("port has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				default:
					r.Log.Info("an unexpected error occurred deleting port", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
			}
		}
		requeue = true
	}

	if lb.Status.PortName != nil { //nolint:dupl // no dupl
		var portByName *ports.Port

		portByName, err = openstackhelper.GetPortByName(ctx, portClient, *lb.Status.PortName)
		if err != nil {
			r.Log.Info("an error occurred extracting ports", "lb", lb.Namespace+"/"+lb.Name)
			return false, err
		}

		if portByName == nil {
			r.Log.Info("no port found, port has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "portName", *lb.Status.PortName)
			err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "portName")
			if err != nil {
				return false, err
			}
		}

		if portByName != nil {
			err = openstackhelper.DeletePort(ctx, portClient, portByName.ID)
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("error deleting port, already deleted", "lb", lb.Namespace+"/"+lb.Name, "portID", portByName.ID)
				default:
					r.Log.Info("an unexpected error occurred deleting port", "lb", lb.Namespace+"/"+lb.Name, "portID", portByName.ID)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
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
		return false, err
	}

	groupClient, err := osClient.GroupClient(ctx)
	if err != nil {
		return false, err
	}

	err = r.findAndDeleteSecGroupUsages(ctx, portClient, lb)
	if err != nil {
		return false, err
	}

	var requeue bool

	if lb.Status.SecurityGroupID != nil {
		var secGroup *groups.SecGroup
		secGroup, err = openstackhelper.GetSecGroupByID(ctx, groupClient, *lb.Status.SecurityGroupID)
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("secGroup has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "security_group_id")
				if err != nil {
					return false, err
				}
			default:
				r.Log.Info("an unexpected error occurred retrieving secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				return false, err
			}
		}
		if secGroup != nil {
			err = openstackhelper.DeleteSecGroup(ctx, groupClient, secGroup.ID)
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("secGroup has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				default:
					r.Log.Info("an unexpected error occurred deleting secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
			}
		}
		requeue = true
	}

	if lb.Status.SecurityGroupName != nil { //nolint:dupl // no dupl
		var secGroupByName *groups.SecGroup

		secGroupByName, err = openstackhelper.GetSecGroupByName(ctx, groupClient, *lb.Status.SecurityGroupName)
		if err != nil {
			r.Log.Info("an error occurred extracting secgroup", "lb", lb.Namespace+"/"+lb.Name)
			return false, err
		}

		if secGroupByName == nil {
			r.Log.Info("no secGroup found, secGroup has already been deleted", "lb",
				lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupName)
			err = helper.RemoveFromLBStatus(ctx, r.Status(), lb, "security_group_name")
			if err != nil {
				return false, err
			}
		}

		if secGroupByName != nil {
			err = openstackhelper.DeleteSecGroup(ctx, groupClient, secGroupByName.ID)
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("error deleting secGroup, already deleted", "lb", lb.Namespace+"/"+lb.Name, "secGroup", secGroupByName.ID)
				default:
					r.Log.Info("an unexpected error occurred deleting secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", secGroupByName.ID)
					return false, kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
				}
			}
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
		return err
	}

	for i := range listedPorts {
		err = openstackhelper.RemoveSecGroupFromPortIfNeeded(ctx, portClient, &listedPorts[i], *lb.Status.SecurityGroupID)
		if err != nil {
			return kubernetes.SendErrorAsEvent(r.RecorderLB, err, lb)
		}
	}

	return nil
}
