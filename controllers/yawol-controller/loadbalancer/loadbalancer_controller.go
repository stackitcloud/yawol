package loadbalancer

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stackitcloud/yawol/internal/openstack"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	coreV1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/labels"
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
	HashLabel          = "lbm-template-hash"
	ServiceFinalizer   = "yawol.stackit.cloud/controller2"
	OpenStackAPIDelay  = 10 * time.Second
	LoadBalancerKind   = "LoadBalancer"
)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	Recorder           record.EventRecorder
	RecorderLB         record.EventRecorder
	skipReconciles     bool
	skipAllButNN       *types.NamespacedName
	MigrateFromOctavia bool
	OpenstackMetrics   prometheus.CounterVec
	getOsClientForIni  func(iniData []byte) (openstack.Client, error)
	WorkerCount        int
	OpenstackTimeout   time.Duration
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
func (r *LoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancer", req.NamespacedName)
	if r.skipReconciles || (r.skipAllButNN != nil && r.skipAllButNN.Name != req.Name && r.skipAllButNN.Namespace != req.Namespace) {
		r.Log.Info("Skip reconciles enabled.. requeuing")
		return ctrl.Result{RequeueAfter: 10 * time.Millisecond}, nil
	}

	var lb yawolv1beta1.LoadBalancer
	if err := r.Client.Get(ctx, req.NamespacedName, &lb); err != nil {
		// Throw if error more serve than 404
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		r.Log.Info("LoadBalancer not found", "lb", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	secretRef := lb.Spec.Infrastructure.AuthSecretRef
	var infraSecret coreV1.Secret
	if err := r.Client.Get(ctx, client.ObjectKey{Name: secretRef.Name, Namespace: secretRef.Namespace}, &infraSecret); err != nil {
		r.Log.Error(err, "could not get infrastructure secret")
		return ctrl.Result{}, err
	}
	var osClient openstack.Client
	if _, ok := infraSecret.Data["cloudprovider.conf"]; !ok {
		return ctrl.Result{}, fmt.Errorf("cloudprovider.conf not found in secret")
	}
	var err error
	var res ctrl.Result
	if osClient, err = r.getOsClientForIni(infraSecret.Data["cloudprovider.conf"]); err != nil {
		return ctrl.Result{}, err
	}

	if lb.DeletionTimestamp != nil {
		if res, err = r.deletionRoutine(ctx, &lb, osClient); err != nil || res.Requeue || res.RequeueAfter != 0 {
			return res, err
		}
		if err = r.removeFinalizer(ctx, lb, ServiceFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if !hasFinalizer(lb.ObjectMeta, ServiceFinalizer) {
		if err = r.addFinalizer(ctx, lb, ServiceFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}
	openstackReconcileHash, err := getOpenStackReconcileHash(&lb)
	if err != nil {
		r.Log.Error(err, "could not generate OpenStackReconcileHash")
		return ctrl.Result{}, err
	}
	before5min := metaV1.Time{Time: time.Now().Add(-5 * time.Minute)}
	// remove openstack reconcile in case of:
	// - lastOpenstackReconcile is older than 5 mins
	// - loadbalancer is in initial creation and not ready
	// - internalLB but has FloatingIP
	// - no internalLB but has non FloatingIP
	// - OpenstackReconcileHash is nil or has changed
	if lb.Status.LastOpenstackReconcile.Before(&before5min) ||
		(lb.Status.ExternalIP == nil && lb.Status.ReadyReplicas != nil && *lb.Status.ReadyReplicas > 0) ||
		(lb.Spec.InternalLB && (lb.Status.FloatingID != nil || lb.Status.FloatingName != nil)) ||
		(!lb.Spec.InternalLB && (lb.Status.FloatingID == nil || lb.Status.FloatingName == nil)) ||
		(lb.Status.OpenstackReconcileHash == nil || *lb.Status.OpenstackReconcileHash != openstackReconcileHash) {
		err = r.removeFromLBStatus(ctx, &lb, "lastOpenstackReconcile")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// run octavia reconcile if lastOpenstackReconcile is nil
	if lb.Status.LastOpenstackReconcile == nil {
		var requeue bool
		var requeueAfter time.Duration

		res, err = r.reconcileSecGroup(ctx, req, &lb, osClient)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res.Requeue {
			requeue = true
		}
		if requeueAfter < res.RequeueAfter {
			requeueAfter = res.RequeueAfter
		}

		if !lb.Spec.InternalLB {
			res, err = r.reconcileFIP(ctx, req, &lb, osClient)
			if err != nil {
				return ctrl.Result{}, err
			}
			if res.Requeue {
				requeue = true
			}
			if requeueAfter < res.RequeueAfter {
				requeueAfter = res.RequeueAfter
			}
		} else {
			var fipClient openstack.FipClient
			fipClient, err = osClient.FipClient()
			if err != nil {
				return ctrl.Result{}, err
			}

			res, err = r.deleteFips(ctx, fipClient, &lb)
			if err != nil {
				return ctrl.Result{}, err
			}
			if res.Requeue {
				requeue = true
			}
			if requeueAfter < res.RequeueAfter {
				requeueAfter = res.RequeueAfter
			}
		}

		res, err = r.reconcilePort(ctx, req, &lb, osClient)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res.Requeue {
			requeue = true
		}
		if requeueAfter < res.RequeueAfter {
			requeueAfter = res.RequeueAfter
		}

		if !lb.Spec.InternalLB {
			res, err = r.reconcileFIPAssociate(ctx, req, &lb, osClient)
			if err != nil {
				return ctrl.Result{}, err
			}
			if res.Requeue {
				requeue = true
			}
			if requeueAfter < res.RequeueAfter {
				requeueAfter = res.RequeueAfter
			}
		}

		if requeue {
			return ctrl.Result{Requeue: requeue}, nil
		}
		if requeueAfter != 0 {
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}

		timeNow := metaV1.Now()
		err = r.patchLBStatus(ctx, &lb, yawolv1beta1.LoadBalancerStatus{LastOpenstackReconcile: &timeNow})
		if err != nil {
			return ctrl.Result{}, err
		}

		// update status lb status accordingly
		openstackReconcileHash, err = getOpenStackReconcileHash(&lb)
		if err != nil {
			return ctrl.Result{}, err
		}

		if lb.Status.OpenstackReconcileHash == nil ||
			*lb.Status.OpenstackReconcileHash != openstackReconcileHash {
			if err = r.patchLBStatus(ctx, &lb, yawolv1beta1.LoadBalancerStatus{
				OpenstackReconcileHash: &openstackReconcileHash,
			}); err != nil {
				r.Log.Error(err, "failed to update OpenstackReconcileHash in status")
				return ctrl.Result{RequeueAfter: DefaultRequeueTime}, r.sendEvent(err, &lb)
			}
		}
	}

	// lbs reconcile is not affected by lastOpenstackReconcile
	if res, err := r.reconcileLoadBalancerSet(ctx, &lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.getOsClientForIni == nil {
		r.getOsClientForIni = func(iniData []byte) (openstack.Client, error) {
			osClient := openstack.OSClient{}
			err := osClient.Configure(iniData)
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

func (r *LoadBalancerReconciler) patchLBStatus(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	lbStatus yawolv1beta1.LoadBalancerStatus,
) error {
	lbStatusJson, _ := json.Marshal(lbStatus)
	patch := []byte(`{"status":` + string(lbStatusJson) + `}`)
	return r.Client.Status().Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerReconciler) removeFromLBStatus(ctx context.Context, lb *yawolv1beta1.LoadBalancer, key string) error {
	patch := []byte(`{"status":{"` + key + `": null}}`)
	return r.Client.Status().Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerReconciler) reconcileFIP(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	var fipClient openstack.FipClient
	var portClient openstack.PortClient
	var lbClient openstack.LoadBalancerClient
	var requeue bool
	{
		var err error
		fipClient, err = osClient.FipClient()
		if err != nil {
			return ctrl.Result{}, err
		}
		portClient, err = osClient.PortClient()
		if err != nil {
			return ctrl.Result{}, err
		}
		lbClient, err = osClient.LoadBalancerClient()
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if r.MigrateFromOctavia &&
		lb.Status.FloatingID == nil &&
		lb.Status.FloatingName == nil &&
		lb.Spec.ExternalIP != nil {
		usedByOctavia, fipId, octaviaID, err := r.isFipIsUsedByOctavia(ctx, fipClient, portClient, lb.Spec.ExternalIP)
		if err != nil {
			return ctrl.Result{}, err
		}
		if usedByOctavia {
			r.RecorderLB.Event(lb, coreV1.EventTypeNormal, "OctaviaMigration", "Found Octavia LB active. Reusing IP.")
			var fip *floatingips.FloatingIP
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			defer cancel()
			fip, err = fipClient.Update(tctx, fipId, floatingips.UpdateOpts{
				PortID: nil,
			})

			if err != nil {
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
			err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
				FloatingID:   &fip.ID,
				FloatingName: &fip.Description,
			})
			if err != nil {
				return ctrl.Result{}, err
			}

			{
				tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
				defer cancel()
				err = lbClient.Delete(tctx, octaviaID, loadbalancers.DeleteOpts{Cascade: true})
				if err != nil {
					return ctrl.Result{}, r.sendEvent(err, lb)
				}
			}
			r.RecorderLB.Event(lb, coreV1.EventTypeNormal, "OctaviaMigration", "Deleted Octavia LoadBalancer.")
		}
	}

	// Patch Floating Name, so we can reference it later
	if lb.Status.FloatingName == nil {
		if err := r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			FloatingName: pointer.StringPtr(req.NamespacedName.String()),
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	var fip *floatingips.FloatingIP
	{
		if lb.Status.FloatingID == nil {
			var err error
			fip, err = r.getFIPByName(ctx, fipClient, *lb.Status.FloatingName)
			if err != nil {
				return ctrl.Result{}, err
			}
			if fip != nil {
				if err := r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{FloatingID: &fip.ID}); err != nil {
					return ctrl.Result{}, err
				}
				requeue = true
			}
		}
	}

	if lb.Status.FloatingID == nil {
		var err error
		var res ctrl.Result
		if res, fip, err = r.createFIP(ctx, fipClient, lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
			return res, err
		}
		// double check so status wont be corrupted
		if fip.ID == "" {
			r.Log.Info("fip was successfully created but fip id is empty")
			panic("fip was successfully created but fip id is empty. crashing to prevent corruption of state")
		}

		if err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			FloatingID: &fip.ID,
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	// Get FIP
	var err error
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	if fip, err = fipClient.Get(tctx, *lb.Status.FloatingID); err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault404:
			r.Log.Info("fip not found in openstack", "fip", *lb.Status.FloatingID)
			if err = r.removeFromLBStatus(ctx, lb, "floatingID"); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
		default:
			r.Log.Info("unexpected error occurred")
			return ctrl.Result{}, r.sendEvent(err, lb)
		}
	}

	if lb.Status.ExternalIP == nil || *lb.Status.ExternalIP != fip.FloatingIP {
		if err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			ExternalIP: &fip.FloatingIP,
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}
	return ctrl.Result{}, nil
}

// isFipIsUsedByOctavia returns true if Fip is used by octavia. If true it returns also the fipID and octaviaID
func (r *LoadBalancerReconciler) isFipIsUsedByOctavia(
	ctx context.Context,
	fipClient openstack.FipClient,
	portClient openstack.PortClient,
	fip *string,
) (used bool, fipID, octaviaID string, err error) {
	if fip == nil {
		return false, "", "", nil
	}

	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	floatingIPs, err := fipClient.List(tctx, floatingips.ListOpts{
		FloatingIP: *fip,
	})
	if err != nil {
		return false, "", "", err
	}

	if len(floatingIPs) < 1 {
		return false, "", "", nil
	}

	if len(floatingIPs) > 1 {
		panic("floating ip is in use... at least twice! WTF?")
	}

	floatingIP := floatingIPs[0]
	if floatingIP.PortID == "" {
		return false, "", "", nil
	}

	{
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		port, err := portClient.Get(tctx, floatingIP.PortID)
		if err != nil {
			return false, "", "", err
		}

		if port.DeviceOwner == "Octavia" {
			lbID := strings.TrimPrefix(port.DeviceID, "lb-")
			return true, floatingIP.ID, lbID, nil
		}
	}

	return false, "", "", nil
}

// Returns a FIP filtered By Name
// Returns an error on connection issues
// Returns nil if not found
func (r *LoadBalancerReconciler) getFIPByName(
	ctx context.Context, fipClient openstack.FipClient, fipName string,
) (*floatingips.FloatingIP, error) {
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	fipList, err := fipClient.List(tctx, floatingips.ListOpts{
		Description: fipName,
	})
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		return nil, err
	}

	for _, floatingIP := range fipList {
		if floatingIP.Description == fipName {
			return &floatingIP, nil
		}
	}

	return nil, nil
}

// Returns a Port filtered By Name
// Returns an error on connection issues
// Returns nil if not found
func (r *LoadBalancerReconciler) getPortByName(ctx context.Context, portClient openstack.PortClient, portName string) (*ports.Port, error) {
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	portList, err := portClient.List(tctx, ports.ListOpts{
		Name: portName,
	})
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		return nil, err
	}

	for _, port := range portList {
		if port.Name == portName {
			return &port, nil
		}
	}

	return nil, nil
}

// Returns a Port filtered By Name
// Returns an error on connection issues
// Returns nil if not found
func (r *LoadBalancerReconciler) getSecGroupByName(
	ctx context.Context, groupClient openstack.GroupClient, groupName string,
) (*groups.SecGroup, error) {
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	groupList, err := groupClient.List(tctx, groups.ListOpts{Name: groupName})
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		return nil, err
	}

	for _, group := range groupList {
		if group.Name == groupName {
			return &group, nil
		}
	}

	return nil, nil
}

func (r *LoadBalancerReconciler) createFIP(
	ctx context.Context,
	fipClient openstack.FipClient,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, *floatingips.FloatingIP, error) {
	desiredFip := ""
	if lb.Spec.ExternalIP != nil {
		desiredFip = *lb.Spec.ExternalIP
	}

	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	fip, err := fipClient.Create(tctx, floatingips.CreateOpts{
		Description:       *lb.Status.FloatingName,
		FloatingNetworkID: *lb.Spec.Infrastructure.FloatingNetID,
		FloatingIP:        desiredFip,
	})
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault400:
			r.Log.Info("desired floating ip is not in range of the floating subnet", "lb", lb.Namespace+"/"+lb.Name, "desiredFIP", desiredFip)
			return ctrl.Result{}, nil, r.sendEvent(err, lb)
		case gophercloud.ErrDefault409:
			r.Log.Info("desired floating ip is already in use", "lb", lb.Namespace+"/"+lb.Name, "desiredFIP", desiredFip)
			return ctrl.Result{}, nil, r.sendEvent(err, lb)
		default:
			r.Log.Info("unexpected error occurred claiming a fip")
			return ctrl.Result{}, nil, r.sendEvent(err, lb)
		}
	}
	return ctrl.Result{}, fip, nil
}

func (r *LoadBalancerReconciler) reconcilePort(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	var requeue bool
	var portClient openstack.PortClient
	{
		var err error
		portClient, err = osClient.PortClient()
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Patch Floating Name, so we can reference it later
	if lb.Status.PortName == nil {
		if err := r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			PortName: pointer.StringPtr(req.NamespacedName.String()),
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	// Reuse port id if found
	var port *ports.Port
	{
		if lb.Status.PortID == nil {
			var err error
			port, err = r.getPortByName(ctx, portClient, *lb.Status.PortName)
			r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
			if err != nil {
				return ctrl.Result{}, err
			}
			if port != nil {
				r.Log.Info("found port with name", "id", port.ID, "lb", req.NamespacedName, "portName", *lb.Status.PortName)
				if err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{PortID: &port.ID}); err != nil {
					return ctrl.Result{}, err
				}
				requeue = true
			}
		}
	}

	// Create Port
	if lb.Status.PortID == nil {
		var err error
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		port, err = portClient.Create(tctx, ports.CreateOpts{
			Name:      *lb.Status.PortName,
			NetworkID: lb.Spec.Infrastructure.NetworkID,
		})
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			switch err.(type) {
			default:
				r.Log.Info("unexpected error occurred claiming a port", "lb", req.NamespacedName)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}

		// double check so status wont be corrupted
		if port.ID == "" {
			r.Log.Info("port was successfully created but port id is empty", "lb", req.NamespacedName)
			panic("port was successfully created but port id is empty. crashing to prevent corruption of state")
		}

		r.Log.Info("successfully created port", "id", port.ID, "lb", req.NamespacedName)

		if err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			PortID: &port.ID,
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	// Check if port still exists properly
	if lb.Status.PortID != nil {
		var err error
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		if port, err = portClient.Get(tctx, *lb.Status.PortID); err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("port not found in openstack", "portID", *lb.Status.PortID)
				if err = r.removeFromLBStatus(ctx, lb, "portID"); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: DefaultRequeueTime}, r.sendEvent(err, lb)
			default:
				r.Log.Info("unexpected error occurred")
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}

		// check if security groups are attached to port
		if lb.Status.SecurityGroupID != nil &&
			(len(port.SecurityGroups) != 1 || port.SecurityGroups[0] != *lb.Status.SecurityGroupID) {
			r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			defer cancel()
			if _, err := portClient.Update(tctx, port.ID, ports.UpdateOpts{
				SecurityGroups: &[]string{*lb.Status.SecurityGroupID},
			}); err != nil {
				r.Log.Error(err, "could not update port.securitygroups", "lb", lb)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
			requeue = true
		}
	}

	if port == nil {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// If internal LB, use first port ip as external ip
	if lb.Spec.InternalLB && len(port.FixedIPs) >= 1 &&
		(lb.Status.ExternalIP == nil || *lb.Status.ExternalIP != port.FixedIPs[0].IPAddress) {
		if err := r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			ExternalIP: &port.FixedIPs[0].IPAddress,
		}); err != nil {
			return ctrl.Result{}, err
		}

		requeue = true
	}

	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) reconcileSecGroup(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	var requeue bool
	var groupClient openstack.GroupClient
	{
		var err error
		groupClient, err = osClient.GroupClient()
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	var ruleClient openstack.RuleClient
	{
		var err error
		ruleClient, err = osClient.RuleClient()
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Patch Floating Name, so we can reference it later
	if lb.Status.SecurityGroupName == nil {
		if err := r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			SecurityGroupName: pointer.StringPtr(req.NamespacedName.String()),
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	// Reuse SecGroup if SecGroup found by name
	var secGroup *groups.SecGroup
	{
		if lb.Status.SecurityGroupID == nil {
			var err error
			secGroup, err = r.getSecGroupByName(ctx, groupClient, *lb.Status.SecurityGroupName)
			if err != nil {
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
			if secGroup != nil {
				if err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{SecurityGroupID: &secGroup.ID}); err != nil {
					return ctrl.Result{}, err
				}
				requeue = true
			}
		}
	}

	// Create SecGroup
	if lb.Status.SecurityGroupID == nil {
		var err error
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		secGroup, err = groupClient.Create(tctx, groups.CreateOpts{
			Name: req.NamespacedName.String(),
		})
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			switch err.(type) {
			default:
				r.Log.Info("unexpected error occurred claiming a fip", "lb", req.NamespacedName)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
		// double check so status wont be corrupted
		if secGroup.ID == "" {
			r.Log.Info("secGroup was successfully created but secGroup id is empty", "lb", req.NamespacedName)
			panic("secGroup was successfully created but secGroup id is empty. crashing to prevent corruption of state")
		}
		if err = r.patchLBStatus(ctx, lb, yawolv1beta1.LoadBalancerStatus{
			SecurityGroupID: &secGroup.ID,
		}); err != nil {
			return ctrl.Result{}, err
		}
		requeue = true
	}

	// Fetch SecGroup for ID
	if lb.Status.SecurityGroupID != nil {
		var err error
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		if secGroup, err = groupClient.Get(tctx, *lb.Status.SecurityGroupID); err != nil {
			r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("SecurityGroupID not found in openstack", "SecurityGroupID", *lb.Status.SecurityGroupID)
				if err = r.removeFromLBStatus(ctx, lb, "security_group_id"); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: DefaultRequeueTime}, r.sendEvent(err, lb)
			default:
				r.Log.Info("unexpected error occurred")
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
	}

	if secGroup == nil {
		r.Log.Info("SecGroup is nil, cannot create rules")
		return ctrl.Result{}, errors.New("SecGroup is nil, cannot create rules")
	}

	// Desired SecGroupRules are:
	// IPv4 and IPv6 all:
	// Egress
	// VRRP Ingress from same secGroup
	desiredSecGroups := []rules.SecGroupRule{}
	etherTypes := []rules.RuleEtherType{rules.EtherType4, rules.EtherType6}
	for _, etherType := range etherTypes {
		desiredSecGroups = append(
			desiredSecGroups,
			rules.SecGroupRule{
				EtherType: string(etherType),
				Direction: string(rules.DirEgress),
			},
			rules.SecGroupRule{
				EtherType:     string(etherType),
				Direction:     string(rules.DirIngress),
				Protocol:      string(rules.ProtocolVRRP),
				RemoteGroupID: secGroup.ID,
			},
			rules.SecGroupRule{
				EtherType:    string(etherType),
				Direction:    string(rules.DirIngress),
				Protocol:     string(rules.ProtocolICMP),
				PortRangeMin: 0,
				PortRangeMax: 8,
			},
		)
	}

	sourceRanges := lb.Spec.LoadBalancerSourceRanges
	if sourceRanges == nil || len(sourceRanges) < 1 {
		// if no range is given, allow all
		sourceRanges = []string{"0.0.0.0/0", "::/0"}
	}

	sshPortUsed := false
	for _, port := range lb.Spec.Ports {
		for _, cidr := range sourceRanges {
			// validate CIDR and ignore if invalid
			_, _, err := net.ParseCIDR(cidr)
			if err != nil {
				r.RecorderLB.Event(
					lb, "Warning", "Error",
					"Could not parse LoadBalancerSourceRange: "+cidr,
				)
				continue
			}

			var etherType rules.RuleEtherType
			if strings.Contains(cidr, ".") {
				etherType = rules.EtherType4
			} else if strings.Contains(cidr, ":") {
				etherType = rules.EtherType6
			}

			portValue := int(port.Port)
			rule := rules.SecGroupRule{
				Direction:      string(rules.DirIngress),
				EtherType:      string(etherType),
				PortRangeMin:   portValue,
				PortRangeMax:   portValue,
				RemoteIPPrefix: cidr,
				Protocol:       string(port.Protocol),
			}

			if portValue == 22 {
				sshPortUsed = true
			}

			desiredSecGroups = append(desiredSecGroups, rule)
		}
	}

	// allow ssh traffic from everywhere when debugging is enabled
	if lb.Spec.DebugSettings.Enabled {
		if sshPortUsed {
			r.RecorderLB.Event(
				lb, "Warning", "Warning",
				"DebugSettings are enabled but port 22 is already in use.",
			)
		} else {
			r.RecorderLB.Event(
				lb, "Warning", "Warning",
				"DebugSettings are enabled, Port 22 is open to all IP ranges.",
			)
			desiredSecGroups = append(
				desiredSecGroups,
				rules.SecGroupRule{
					Direction:      string(rules.DirIngress),
					EtherType:      string(rules.EtherType4),
					RemoteIPPrefix: "0.0.0.0/0",
					PortRangeMin:   22,
					PortRangeMax:   22,
					Protocol:       string(rules.ProtocolTCP),
				},
				rules.SecGroupRule{
					Direction:      string(rules.DirIngress),
					EtherType:      string(rules.EtherType6),
					RemoteIPPrefix: "::/0",
					PortRangeMin:   22,
					PortRangeMax:   22,
					Protocol:       string(rules.ProtocolTCP),
				},
			)
		}
	}

	// delete rules that are not used anymore
	// deletion must happen before creation in order
	// to not have temporary duplicated / overlapping rules
	for _, appliedRule := range secGroup.Rules {
		found := false
		for _, desiredRule := range desiredSecGroups {
			if secGroupRuleIsEqual(appliedRule, desiredRule) {
				found = true
				break
			}
		}

		if found {
			continue
		}

		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		// rule not found -> delete
		if err := ruleClient.Delete(tctx, appliedRule.ID); err != nil {
			cancel()
			r.Log.Error(err, "failed to delete secgroup rule", "secgroup", secGroup.ID, "rule", appliedRule)
			return ctrl.Result{RequeueAfter: DefaultRequeueTime}, r.sendEvent(err, lb)
		}
		cancel()
	}

	// create non existing rules
	for _, desiredRule := range desiredSecGroups {
		var isApplied bool
		for _, appliedRule := range secGroup.Rules {
			if secGroupRuleIsEqual(appliedRule, desiredRule) {
				isApplied = true
				break
			}
		}

		if !isApplied {
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			_, err := ruleClient.Create(tctx, rules.CreateOpts{
				SecGroupID:     secGroup.ID,
				Description:    req.NamespacedName.String(),
				Direction:      rules.RuleDirection(desiredRule.Direction),
				EtherType:      rules.RuleEtherType(desiredRule.EtherType),
				Protocol:       rules.RuleProtocol(desiredRule.Protocol),
				PortRangeMax:   desiredRule.PortRangeMax,
				PortRangeMin:   desiredRule.PortRangeMin,
				RemoteIPPrefix: desiredRule.RemoteIPPrefix,
				RemoteGroupID:  desiredRule.RemoteGroupID,
			})
			cancel()
			r.OpenstackMetrics.WithLabelValues("Neutron").Inc()

			if err != nil {
				r.Log.Error(err, "failed to apply secgroup rule", "secgroup", secGroup.ID, "rule", desiredRule)
				return ctrl.Result{RequeueAfter: DefaultRequeueTime}, r.sendEvent(err, lb)
			}
		}
	}

	if requeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	return ctrl.Result{}, nil
}

func secGroupRuleIsEqual(first, second rules.SecGroupRule) bool {
	if first.RemoteIPPrefix == second.RemoteIPPrefix &&
		first.EtherType == second.EtherType &&
		first.PortRangeMax == second.PortRangeMax &&
		first.PortRangeMin == second.PortRangeMin &&
		first.Protocol == second.Protocol &&
		first.RemoteGroupID == second.RemoteGroupID {
		return true
	}

	return false
}

func (r *LoadBalancerReconciler) reconcileFIPAssociate(
	ctx context.Context,
	req ctrl.Request,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	var fipClient openstack.FipClient
	{
		var err error
		fipClient, err = osClient.FipClient()
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if lb.Status.PortID == nil || lb.Status.FloatingID == nil {
		r.Log.WithName("reconcileFIPAssociate").Info("either portID or floatingID is null", "lb", req)
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	{
		var fip *floatingips.FloatingIP
		var err error
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		if fip, err = fipClient.Get(tctx, *lb.Status.FloatingID); err != nil {
			return ctrl.Result{}, r.sendEvent(err, lb)
		}

		if fip.PortID == *lb.Status.PortID {
			return ctrl.Result{}, nil
		}
	}

	// ports are not in sync -> update
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	if _, err := fipClient.Update(tctx, *lb.Status.FloatingID, floatingips.UpdateOpts{
		PortID: lb.Status.PortID,
	}); err != nil {
		r.Log.
			WithName("reconcileFIPAssociate").
			Info("failed to associate fip with port",
				"lb", req, "fip",
				*lb.Status.FloatingID, "port", *lb.Status.PortID,
			)
		return ctrl.Result{}, r.sendEvent(err, lb)
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) reconcileLoadBalancerSet(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	// Get the labels LBS will receive on creation from this lb
	lbsLabels := r.getLoadBalancerSetLabelsFromLoadBalancer(lb)

	// Make sure LoadBalancer has revision number
	var currentRevision int
	if currentRevision = r.readCurrentRevisionFromLB(lb); currentRevision == 0 {
		if err := r.patchLoadBalancerRevision(ctx, lb, 1); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// Get Hash for current LoadBalancerMachineSpec
	var hash string
	{
		var err error
		floatingID := ""
		if lb.Status.FloatingID != nil {
			floatingID = *lb.Status.FloatingID
		}

		if hash, err = hashData(yawolv1beta1.LoadBalancerMachineSpec{
			Infrastructure: lb.Spec.Infrastructure,
			FloatingID:     floatingID,
			LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
				Namespace: lb.Namespace,
				Name:      lb.Name,
			},
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Get LoadBalancerSet for current hash
	var loadBalancerSet *yawolv1beta1.LoadBalancerSet
	{
		var err error
		if loadBalancerSet, err = r.getLoadBalancerSetForHash(ctx, lbsLabels, hash); err != nil {
			return ctrl.Result{}, err
		}

		// set to empty object to prevent nil pointer exception
		if loadBalancerSet == nil {
			loadBalancerSet = &yawolv1beta1.LoadBalancerSet{}
		}
	}

	// create new lbset if hash changed
	if loadBalancerSet.Name == "" {
		newRevision, err := r.getNextRevisionFromLB(ctx, lb)
		if err != nil {
			return ctrl.Result{}, err
		}

		floatingID := ""
		if lb.Status.FloatingID != nil {
			floatingID = *lb.Status.FloatingID
		}

		var res ctrl.Result
		if res, err = r.createLoadBalancerSet(ctx, lb, yawolv1beta1.LoadBalancerMachineSpec{
			Infrastructure: lb.Spec.Infrastructure,
			FloatingID:     floatingID,
			LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
				Namespace: lb.Namespace,
				Name:      lb.Name,
			},
		}, hash, newRevision); err != nil {
			return res, err
		}

		err = r.patchLoadBalancerRevision(ctx, lb, newRevision)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// loadBalancerSet is never a newly created one from here on
	// because we early return with a requeue after creation

	// update lb revision to lbset revision
	// in order to show the correct status on the lb object
	lbsRevision := r.readRevisionFromLBS(loadBalancerSet)
	if lbsRevision != currentRevision {
		r.Log.Info("patch lb revision to match lbs revision", "revision", lbsRevision)
		err := r.patchLoadBalancerRevision(ctx, lb, lbsRevision)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// update replicas
	if loadBalancerSet.Spec.Replicas != lb.Spec.Replicas {
		err := r.patchLoadBalancerSetReplicas(ctx, loadBalancerSet, lb.Spec.Replicas)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// check if all replicas from current lbset are ready
	{
		ready, err := r.loadBalancerSetIsReady(ctx, lb, loadBalancerSet)
		if err != nil {
			return ctrl.Result{}, err
		}

		if !ready {
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		r.Log.Info("current lbset is ready", "lbs", loadBalancerSet.Name)
	}

	// scale down after upscale to ensure no downtime
	{
		downscaled, err := r.areAllLoadBalancerSetsForLBButDownscaled(ctx, lb, loadBalancerSet.Name)
		if err != nil {
			return ctrl.Result{}, err
		}

		if !downscaled {
			r.Log.Info("scale down all lbsets except of", "lbs", loadBalancerSet.Name)
			return r.scaleDownAllLoadBalancerSetsForLBBut(ctx, lb, loadBalancerSet.Name)
		}
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) patchLoadBalancerRevision(ctx context.Context, lb *yawolv1beta1.LoadBalancer, revision int) error {
	if revision < 1 {
		return errors.New("revision number for lb must be >0")
	}

	patch := []byte(`{"metadata": {"annotations": {"` + RevisionAnnotation + `": "` + strconv.Itoa(revision) + `"} }}`)
	return r.Client.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func hashData(data interface{}) (string, error) {
	var jsonSpec []byte
	var err error
	if jsonSpec, err = json.Marshal(data); err != nil {
		return "", err
	}

	bytes := sha256.Sum256(jsonSpec)
	return strings.ToLower(base32.StdEncoding.EncodeToString(bytes[:]))[:16], nil
}

func copyLabelMap(lbs map[string]string) map[string]string {
	targetMap := make(map[string]string)
	for key, value := range lbs {
		targetMap[key] = value
	}
	return targetMap
}

func (r *LoadBalancerReconciler) getLoadBalancerSetLabelsFromLoadBalancer(lb *yawolv1beta1.LoadBalancer) map[string]string {
	return copyLabelMap(lb.Spec.Selector.MatchLabels)
}

func (r *LoadBalancerReconciler) patchLoadBalancerSetReplicas(ctx context.Context, lbs *yawolv1beta1.LoadBalancerSet, replicas int) error {
	patch := []byte(`{"spec": {"replicas": ` + strconv.Itoa(replicas) + `}}`)
	return r.Client.Patch(ctx, lbs, client.RawPatch(types.MergePatchType, patch))
}

func getOwnersReferenceForLB(lb *yawolv1beta1.LoadBalancer) metaV1.OwnerReference {
	return metaV1.OwnerReference{
		APIVersion: lb.APIVersion,
		Kind:       lb.Kind,
		Name:       lb.Name,
		UID:        lb.UID,
	}
}

// Returns nil if no matching exists
// If there are multiple: Returns one with highest RevisionAnnotation annotation
// If there is a single one: returns the one fetched
func (r *LoadBalancerReconciler) getLoadBalancerSetForHash(
	ctx context.Context,
	filterLabels map[string]string,
	hash string,
) (*yawolv1beta1.LoadBalancerSet, error) {
	filterLabels = copyLabelMap(filterLabels)
	filterLabels[HashLabel] = hash
	var loadBalancerSetList yawolv1beta1.LoadBalancerSetList

	if err := r.Client.List(ctx, &loadBalancerSetList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(filterLabels),
	}); err != nil {
		return nil, err
	}

	if len(loadBalancerSetList.Items) == 0 {
		return nil, nil
	}

	if len(loadBalancerSetList.Items) == 1 {
		return &loadBalancerSetList.Items[0], nil
	}

	var highestGen int
	var highestGenLBS *yawolv1beta1.LoadBalancerSet
	if len(loadBalancerSetList.Items) > 1 {
		for _, lbs := range loadBalancerSetList.Items {
			if gen, err := strconv.Atoi(lbs.Annotations[RevisionAnnotation]); err == nil && highestGen < gen {
				highestGen = gen
				highestGenLBS = &lbs
			}
		}
	}

	return highestGenLBS, nil
}

func (r *LoadBalancerReconciler) readRevisionFromLBS(lbs *yawolv1beta1.LoadBalancerSet) int {
	var currentRevision int
	if lbs.Annotations[RevisionAnnotation] == "" {
		return 0
	}

	var err error
	if currentRevision, err = strconv.Atoi(lbs.Annotations[RevisionAnnotation]); err != nil {
		r.Log.Info("failed to read revision on annotation", "annotation", RevisionAnnotation+"="+lbs.Annotations[RevisionAnnotation])
		return 0
	}
	return currentRevision
}

func (r *LoadBalancerReconciler) readCurrentRevisionFromLB(lb *yawolv1beta1.LoadBalancer) int {
	var currentRevision int
	if lb.Annotations[RevisionAnnotation] == "" {
		return 0
	}

	var err error
	if currentRevision, err = strconv.Atoi(lb.Annotations[RevisionAnnotation]); err != nil {
		r.Log.Info("failed to read revision on annotation", "annotation", RevisionAnnotation+"="+lb.Annotations[RevisionAnnotation])
		return 0
	}
	return currentRevision
}

func (r *LoadBalancerReconciler) getNextRevisionFromLB(ctx context.Context, lb *yawolv1beta1.LoadBalancer) (int, error) {
	loadBalancerSetList, err := r.getLoadBalancerSetsForLoadBalancer(ctx, lb)
	if err != nil {
		return 0, err
	}
	var highestRevision int
	for _, loadBalancerSet := range loadBalancerSetList.Items {
		rev := r.readRevisionFromLBS(&loadBalancerSet)
		if rev > highestRevision {
			highestRevision = rev
		}
	}
	highestRevision++
	return highestRevision, nil
}

func (r *LoadBalancerReconciler) createLoadBalancerSet(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	machineSpec yawolv1beta1.LoadBalancerMachineSpec,
	hash string,
	revision int,
) (ctrl.Result, error) {
	lbsetLabels := r.getLoadBalancerSetLabelsFromLoadBalancer(lb)
	lbsetLabels[HashLabel] = hash

	lbset := yawolv1beta1.LoadBalancerSet{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      lb.Name + "-" + hash,
			Namespace: lb.Namespace,
			OwnerReferences: []metaV1.OwnerReference{
				getOwnersReferenceForLB(lb),
			},
			Labels: lbsetLabels,
			Annotations: map[string]string{
				RevisionAnnotation: strconv.Itoa(revision),
			},
		},
		Spec: yawolv1beta1.LoadBalancerSetSpec{
			Selector: metaV1.LabelSelector{
				MatchLabels: lbsetLabels,
			},
			Template: yawolv1beta1.LoadBalancerMachineTemplateSpec{
				Labels: lbsetLabels,
				Spec:   machineSpec,
			},
			Replicas: lb.Spec.Replicas,
		},
	}
	if err := r.Client.Create(ctx, &lbset); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
}

// Scales down all LoadBalancerSets deriving from a LB but the the one with the name of exceptionName
// This will use getLoadBalancerSetsForLoadBalancer to identify the LBS deriving from the given LB
// See error handling getLoadBalancerSetsForLoadBalancer
// See error handling patchLoadBalancerSetReplicas
// Requests requeue when a LBS has been downscaled
func (r *LoadBalancerReconciler) scaleDownAllLoadBalancerSetsForLBBut(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	exceptionName string,
) (ctrl.Result, error) {
	loadBalancerSetList, err := r.getLoadBalancerSetsForLoadBalancer(ctx, lb)
	if err != nil {
		return ctrl.Result{}, err
	}

	var scaledDown bool
	for _, lbSet := range loadBalancerSetList.Items {
		if lbSet.Name != exceptionName && lbSet.Spec.Replicas > 0 {
			if err := r.patchLoadBalancerSetReplicas(ctx, &lbSet, 0); err != nil {
				return ctrl.Result{}, err
			}
			scaledDown = true
		}
	}

	if scaledDown {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}
	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) loadBalancerSetIsReady(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	currentSet *yawolv1beta1.LoadBalancerSet,
) (bool, error) {
	loadBalancerSetList, err := r.getLoadBalancerSetsForLoadBalancer(ctx, lb)
	if err != nil {
		return false, err
	}

	for _, loadBalancerSet := range loadBalancerSetList.Items {
		if loadBalancerSet.Name != currentSet.Name {
			continue
		}

		desiredReplicas := currentSet.Spec.Replicas

		if loadBalancerSet.Spec.Replicas != desiredReplicas {
			return false, nil
		}

		if loadBalancerSet.Status.Replicas == nil ||
			loadBalancerSet.Status.ReadyReplicas == nil {
			return false, nil
		}

		if *loadBalancerSet.Status.Replicas != desiredReplicas ||
			*loadBalancerSet.Status.ReadyReplicas != desiredReplicas {
			return false, nil
		}

		return true, nil
	}

	return false, fmt.Errorf("active LoadBalancerSet not found")
}

// Checks if LoadBalancerSets deriving from LoadBalancers are downscaled except for the LoadBalancerSet with the name of exceptionName
// Returns true if all are downscaled; false if not
// Follows error contract of getLoadBalancerSetsForLoadBalancer
func (r *LoadBalancerReconciler) areAllLoadBalancerSetsForLBButDownscaled(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	exceptionName string,
) (bool, error) {
	loadBalancerSetList, err := r.getLoadBalancerSetsForLoadBalancer(ctx, lb)
	if err != nil {
		return false, err
	}

	for _, loadBalancerSet := range loadBalancerSetList.Items {
		if loadBalancerSet.Name == exceptionName {
			continue
		}
		if loadBalancerSet.Spec.Replicas != 0 {
			return false, nil
		}
		if loadBalancerSet.Status.Replicas != nil && *loadBalancerSet.Status.Replicas != 0 {
			return false, nil
		}
		if loadBalancerSet.Status.ReadyReplicas != nil && *loadBalancerSet.Status.ReadyReplicas != 0 {
			return false, nil
		}
	}

	return true, nil
}

// This returns all LoadBalancerSets for a given LoadBalancer
// Returns an error if lb is nil
// Returns an error if lb.UID is empty
// Returns an error if kube api-server problems occurred
func (r *LoadBalancerReconciler) getLoadBalancerSetsForLoadBalancer(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
) (yawolv1beta1.LoadBalancerSetList, error) {
	if lb == nil {
		return yawolv1beta1.LoadBalancerSetList{}, errors.New("LoadBalancer must no be nil")
	}

	if lb.UID == "" {
		return yawolv1beta1.LoadBalancerSetList{}, errors.New("LoadBalancer.UID must not be nil")
	}

	filerLabels := r.getLoadBalancerSetLabelsFromLoadBalancer(lb)
	var loadBalancerSetList yawolv1beta1.LoadBalancerSetList
	if err := r.Client.List(ctx, &loadBalancerSetList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(filerLabels),
		Namespace:     lb.Namespace,
	}); err != nil {
		return yawolv1beta1.LoadBalancerSetList{}, err
	}

	return loadBalancerSetList, nil
}

func (r *LoadBalancerReconciler) deletionRoutine(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	osClient openstack.Client,
) (ctrl.Result, error) {
	var fipClient openstack.FipClient
	var portClient openstack.PortClient
	var groupClient openstack.GroupClient
	{
		var err error
		fipClient, err = osClient.FipClient()
		if err != nil {
			return ctrl.Result{}, err
		}

		portClient, err = osClient.PortClient()
		if err != nil {
			return ctrl.Result{}, err
		}

		groupClient, err = osClient.GroupClient()
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if list, err := r.getLoadBalancerSetsForLoadBalancer(ctx, lb); err != nil {
		return ctrl.Result{}, err
	} else if len(list.Items) > 0 {
		for _, loadBalancerSet := range list.Items {
			if loadBalancerSet.DeletionTimestamp == nil {
				if err := r.Client.Delete(ctx, &loadBalancerSet); err != nil {
					return ctrl.Result{}, err
				}
			}

			// TODO check if this is wrong
			// nolint:staticcheck // see if this is wrong
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	var err error
	var role rbac.Role
	if lb.Status.NodeRoleRef != nil {
		if err = r.Client.Get(ctx, types.NamespacedName{
			Namespace: lb.Namespace,
			Name:      lb.Status.NodeRoleRef.Name}, &role); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if err = r.removeFromLBStatus(ctx, lb, "nodeRoleRef"); err != nil {
			return ctrl.Result{}, err
		}
	}

	var res ctrl.Result
	if res, err = r.deleteFips(ctx, fipClient, lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}
	if res, err = r.deletePorts(ctx, portClient, lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}
	if res, err = r.deleteSecGroups(ctx, portClient, groupClient, lb); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	return ctrl.Result{}, nil
}

// Deletes floating ips related to the LoadBalancer object
// 1. Retrieves FIP by ID in lb.Status.FloatingID
// 1.1 if found => delete FIP
// 2. Retrieves FIP by Name in lb.Status.FloatingName
// 2.1 if found => delete FIP
// Returns any error except for 404 errors from gophercloud
func (r *LoadBalancerReconciler) deleteFips(
	ctx context.Context,
	fipClient openstack.FipClient,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	var err error
	var fip *floatingips.FloatingIP
	var requeue = false

	if lb.Status.FloatingID != nil {
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		fip, err = fipClient.Get(tctx, *lb.Status.FloatingID)
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("error getting fip, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				err = r.removeFromLBStatus(ctx, lb, "floatingID")
				if err != nil {
					return ctrl.Result{}, err
				}
			default:
				r.Log.Info("an unexpected error occurred retrieving fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
		if fip != nil {
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			defer cancel()
			err = fipClient.Delete(tctx, fip.ID)
			r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					r.Log.Info("error deleting fip, already deleted", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
				default:
					r.Log.Info("an unexpected error occurred deleting fip", "lb", lb.Namespace+"/"+lb.Name, "fipId", *lb.Status.FloatingID)
					return ctrl.Result{}, r.sendEvent(err, lb)
				}
			}

			// requeue so next run will delete the status
			requeue = true
		}
	}

	if lb.Status.FloatingName != nil {
		var fips []floatingips.FloatingIP
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		fips, err = fipClient.List(tctx, floatingips.ListOpts{
			Description: *lb.Status.FloatingName,
		})
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			r.Log.Info("an error occurred extracting floating IPs", "lb", lb.Namespace+"/"+lb.Name)
			return ctrl.Result{}, err
		}

		if len(fips) == 0 {
			r.Log.Info("no fips found, fip has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "fipName", *lb.Status.FloatingName)
			err = r.removeFromLBStatus(ctx, lb, "floatingName")
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		for _, floatingIP := range fips {
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			err = fipClient.Delete(tctx, floatingIP.ID)
			cancel()
			r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
				case gophercloud.ErrDefault409:
					r.Log.Info("could not delete floating IP, retry deletion...", "lb", lb.Namespace+"/"+lb.Name)
					return ctrl.Result{RequeueAfter: OpenStackAPIDelay}, nil
				default:
					r.Log.Info("an error occurred deleting floating IP", "lb", lb.Namespace+"/"+lb.Name)
					return ctrl.Result{}, r.sendEvent(err, lb)
				}
			}
		}

		// requeue so next run will delete the status
		requeue = true
	}

	return ctrl.Result{Requeue: requeue}, nil
}

// Deletes ports related to the LoadBalancer object
// 1. Retrieves port by ID in lb.Status.PortID
// 1.1 if found => delete port
// 2. Retrieves port by Name in lb.Status.PortName
// 2.1 if found => delete port
// Returns any error except for 404 errors from gophercloud
func (r *LoadBalancerReconciler) deletePorts(
	ctx context.Context,
	portClient openstack.PortClient,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	var err error
	var port *ports.Port
	if lb.Status.PortID == nil {
		return ctrl.Result{}, nil
	}

	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	port, err = portClient.Get(tctx, *lb.Status.PortID)
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault404:
			r.Log.Info("port has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
		default:
			r.Log.Info("an unexpected error occurred retrieving port", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
			return ctrl.Result{}, err
		}
	}
	if port != nil {
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		err = portClient.Delete(tctx, port.ID)
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("port has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
			default:
				r.Log.Info("an unexpected error occurred deleting port", "lb", lb.Namespace+"/"+lb.Name, "portID", *lb.Status.PortID)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
	}

	var listedPorts []ports.Port
	ttctx, ccancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer ccancel()
	listedPorts, err = portClient.List(ttctx, ports.ListOpts{
		Name: *lb.Status.PortName,
	})
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		r.Log.Info("an error occurred extracting floating IPs", "lb", lb)
		return ctrl.Result{}, err
	}
	for _, iPort := range listedPorts {
		{
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			err = portClient.Delete(tctx, iPort.ID)
			cancel()
		}
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()

		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
			case gophercloud.ErrDefault409:
				r.Log.Info("could not delete port, retry deletion...", "lb", lb.Namespace+"/"+lb.Name)
				return ctrl.Result{RequeueAfter: OpenStackAPIDelay}, nil
			default:
				r.Log.Info("an error occurred deleting port", "lb", lb.Namespace+"/"+lb.Name)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
	}

	return ctrl.Result{}, nil
}

// Deletes SecGroups related to the LoadBalancer objects an disassociate it from other ports
// 1. Removes sec group from all listable ports
// 2. Retrieves sec group by ID in lb.Status.SecGroupID
// 2.1 if found => delete sec group
// 3. Retrieves sec group by Name in lb.Status.SecGroupName
// 3.1 if found => delete sec group
func (r *LoadBalancerReconciler) deleteSecGroups(
	ctx context.Context,
	portClient openstack.PortClient,
	groupClient openstack.GroupClient,
	lb *yawolv1beta1.LoadBalancer,
) (ctrl.Result, error) {
	var err error
	if lb.Status.SecurityGroupID == nil {
		return ctrl.Result{}, nil
	}

	err = r.findAndDeleteSecGroupUsages(ctx, portClient, lb)
	if err != nil {
		return ctrl.Result{}, err
	}

	var secGroup *groups.SecGroup
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	secGroup, err = groupClient.Get(tctx, *lb.Status.SecurityGroupID)
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		switch err.(type) {
		case gophercloud.ErrDefault404:
			r.Log.Info("secGroup has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
		default:
			r.Log.Info("an unexpected error occurred retrieving secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
			return ctrl.Result{}, err
		}
	}
	if secGroup != nil {
		if err != nil {
			return ctrl.Result{}, err
		}
		{
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			defer cancel()
			err = groupClient.Delete(tctx, secGroup.ID)
		}
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
				r.Log.Info("secGroup has already been deleted", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
			default:
				r.Log.Info("an unexpected error occurred deleting secGroup", "lb", lb.Namespace+"/"+lb.Name, "secGroup", *lb.Status.SecurityGroupID)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
	}

	var secGroups []groups.SecGroup
	{
		tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
		defer cancel()
		secGroups, err = groupClient.List(tctx, groups.ListOpts{
			Name: *lb.Status.SecurityGroupName,
		})
	}
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		r.Log.Info("an error occurred extracting secgroups", "lb", lb.Namespace+"/"+lb.Name)
		return ctrl.Result{}, err
	}
	for _, secGroup := range secGroups {
		if err != nil {
			return ctrl.Result{}, err
		}
		{
			tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
			err = groupClient.Delete(tctx, secGroup.ID)
			cancel()
		}
		r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
		if err != nil {
			switch err.(type) {
			case gophercloud.ErrDefault404:
			case gophercloud.ErrDefault409:
				r.Log.Info("could not delete secGroup, retry deletion...", "lb", lb.Namespace+"/"+lb.Name)
				return ctrl.Result{RequeueAfter: OpenStackAPIDelay}, nil
			default:
				r.Log.Info("an error occurred deleting secGroup", "lb", lb.Namespace+"/"+lb.Name)
				return ctrl.Result{}, r.sendEvent(err, lb)
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) findAndDeleteSecGroupUsages(
	ctx context.Context,
	portClient openstack.PortClient,
	lb *yawolv1beta1.LoadBalancer) error {
	var listedPorts []ports.Port
	tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
	defer cancel()
	listedPorts, err := portClient.List(tctx, ports.ListOpts{})
	r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
	if err != nil {
		return err
	}

	for _, port := range listedPorts {
		for _, securityGroupID := range port.SecurityGroups {
			if securityGroupID == *lb.Status.SecurityGroupID {
				purgedList := removeString(port.SecurityGroups, securityGroupID)
				r.OpenstackMetrics.WithLabelValues("Neutron").Inc()
				{
					tctx, cancel := context.WithTimeout(ctx, r.OpenstackTimeout)
					if _, err = portClient.Update(tctx, port.ID, ports.UpdateOpts{
						SecurityGroups: &purgedList,
					}); err != nil {
						cancel()
						return r.sendEvent(err, lb)
					}
					cancel()
				}
			}
		}
	}

	return nil
}

func hasFinalizer(objectMeta metaV1.ObjectMeta, finalizer string) bool {
	for _, f := range objectMeta.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func (r LoadBalancerReconciler) addFinalizer(ctx context.Context, loadBalancer yawolv1beta1.LoadBalancer, finalizer string) error {
	patch := []byte(`{"metadata":{"finalizers": ["` + finalizer + `"]}}`)
	return r.Client.Patch(ctx, &loadBalancer, client.RawPatch(types.MergePatchType, patch))
}

func (r *LoadBalancerReconciler) removeFinalizer(ctx context.Context, loadBalancer yawolv1beta1.LoadBalancer, finalizer string) error {
	fin, err := json.Marshal(removeString(loadBalancer.Finalizers, finalizer))
	if err != nil {
		return err
	}
	patch := []byte(`{"metadata":{"finalizers": ` + string(fin) + ` }}`)
	return r.Client.Patch(ctx, &loadBalancer, client.RawPatch(types.MergePatchType, patch))
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

func (r *LoadBalancerReconciler) sendEvent(err error, obj ...runtime.Object) error {
	var body []byte
	switch casted := err.(type) {
	case gophercloud.ErrDefault401:
		body = casted.Body
	case gophercloud.ErrDefault403:
		body = casted.Body
	case gophercloud.ErrDefault404:
		body = casted.Body
	case gophercloud.ErrDefault409:
		body = casted.Body
	}

	if len(body) > 0 {
		for _, ob := range obj {
			if ob.GetObjectKind().GroupVersionKind().Kind == LoadBalancerKind {
				r.RecorderLB.Event(ob, coreV1.EventTypeWarning, "Failed", string(body))
			} else {
				r.Recorder.Event(ob, coreV1.EventTypeWarning, "Failed", string(body))
			}
		}
	}
	return err
}

func getOpenStackReconcileHash(lb *yawolv1beta1.LoadBalancer) (string, error) {
	return hashData(map[string]interface{}{
		"ports":         lb.Spec.Ports,
		"sourceRanges":  lb.Spec.LoadBalancerSourceRanges,
		"debugSettings": lb.Spec.DebugSettings,
	})
}
