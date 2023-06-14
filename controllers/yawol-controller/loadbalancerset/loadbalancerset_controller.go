package loadbalancerset

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
)

const FINALIZER = "stackit.cloud/loadbalancermachine"

// LoadBalancerSetReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerSetReconciler struct { //nolint:revive // naming from kubebuilder
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	Metrics     *helpermetrics.LoadBalancerSetMetricList
	WorkerCount int
	RateLimiter ratelimiter.RateLimiter
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
func (r *LoadBalancerSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("LoadBalancerSet", req.NamespacedName)

	// get the parent obj
	var set yawolv1beta1.LoadBalancerSet
	if err := r.Client.Get(ctx, req.NamespacedName, &set); err != nil {
		r.Log.Info("Unable to fetch LoadbalancerSet")
		// ignore not found, cause requeue dont fix this err
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if set.ObjectMeta.DeletionTimestamp != nil {
		return r.deletionRoutine(ctx, &set)
	}

	helper.ParseLoadBalancerSetMetrics(
		set,
		r.Metrics,
	)

	// obj is not being deleted, set finalizer
	kubernetes.AddFinalizerIfNeeded(ctx, r.Client, &set, FINALIZER)

	// get all childes by label
	var childMachines yawolv1beta1.LoadBalancerMachineList
	if err := r.List(ctx, &childMachines, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(set.Spec.Selector.MatchLabels),
		Namespace:     req.Namespace,
	}); err != nil {
		r.Log.Error(err, helper.ErrListingChildLBMs.Error())
		return ctrl.Result{}, err
	}

	var (
		readyMachineCount   int
		deletedMachineCount int
		hasKeepalivedMaster bool
	)

	for i := range childMachines.Items {
		if childMachines.Items[i].DeletionTimestamp != nil {
			deletedMachineCount++
			continue
		}
		if isMachineMaster(childMachines.Items[i]) {
			hasKeepalivedMaster = true
		}

		if isMachineReady(childMachines.Items[i]) {
			readyMachineCount++
			continue
		}
	}

	// TODO: patchStatus *after* we've reconciled the replicas
	if res, err := r.patchStatus(ctx, &set, readyMachineCount, hasKeepalivedMaster); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	if res, err := r.reconcileReplicas(
		ctx,
		&set,
		childMachines.Items,
		deletedMachineCount,
	); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *LoadBalancerSetReconciler) deletionRoutine(
	ctx context.Context,
	set *yawolv1beta1.LoadBalancerSet,
) (ctrl.Result, error) {
	// obj is being deleted
	if containsString(set.ObjectMeta.Finalizers, FINALIZER) {
		// finalizer is present
		if err := r.deleteAllMachines(ctx, set); err != nil {
			return ctrl.Result{}, err
		}
	}
	// check if deletion is finished
	var childMachines yawolv1beta1.LoadBalancerMachineList
	if err := r.List(ctx, &childMachines, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(set.Spec.Selector.MatchLabels),
		Namespace:     set.Namespace,
	}); err != nil {
		r.Log.Error(err, "error in deletion")
		return ctrl.Result{}, err
	}
	if len(childMachines.Items) != 0 {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	helper.RemoveLoadBalancerSetMetrics(
		*set,
		r.Metrics,
	)

	// remove finalizer
	if err := kubernetes.RemoveFinalizerIfNeeded(ctx, r.Client, set, FINALIZER); err != nil {
		return ctrl.Result{}, err
	}

	// stop reconciliation as item is being deleted
	return ctrl.Result{}, nil
}

func (r *LoadBalancerSetReconciler) patchStatus(
	ctx context.Context,
	set *yawolv1beta1.LoadBalancerSet,
	readyMachinesCount int,
	hasKeepalivedMaster bool,
) (ctrl.Result, error) {

	setCopy := set.DeepCopy()

	// Write replicas into status
	set.Status.Replicas = pointer.Int(set.Spec.Replicas)

	// Write ready replicas into status
	set.Status.ReadyReplicas = pointer.Int(readyMachinesCount)

	// Write HasKeepalivedMaster condition
	status := metav1.ConditionFalse
	reason := "NoKeepalivedMasterInSet"
	message := "LoadBalancerSet doesn't have a master machine"

	if hasKeepalivedMaster {
		status = metav1.ConditionTrue
		reason = "KeepalivedMasterInSet"
		message = "LoadBalancerSet has a master machine"
	}

	meta.SetStatusCondition(&set.Status.Conditions, metav1.Condition{
		Type:    helper.HasKeepalivedMaster,
		Status:  status,
		Reason:  reason,
		Message: message,
	})

	if equality.Semantic.DeepEqual(set, setCopy) {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, r.Client.Status().Patch(ctx, set, client.MergeFrom(setCopy))
}

func (r *LoadBalancerSetReconciler) reconcileReplicas(
	ctx context.Context,
	set *yawolv1beta1.LoadBalancerSet,
	machines []yawolv1beta1.LoadBalancerMachine,
	deletedMachineCount int,
) (ctrl.Result, error) {
	if len(machines) < set.Spec.Replicas {
		if err := r.createMachine(ctx, set); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}

	if set.Spec.Replicas < len(machines)-deletedMachineCount {
		machineForDeletion, err := findFirstMachineForDeletion(machines)
		if err != nil {
			return ctrl.Result{}, err
		}

		if err := r.deleteMachine(ctx, &machineForDeletion); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}

	return ctrl.Result{}, nil
}

// findFirstMachineForDeletion returns a machine which should be deleted first in case of a scale down.
// returns unready machines first
// returns machine which is not keepalived master
// returns the first machine
func findFirstMachineForDeletion(machines []yawolv1beta1.LoadBalancerMachine) (yawolv1beta1.LoadBalancerMachine, error) {
	for i := range machines {
		if !isMachineReady(machines[i]) {
			return machines[i], nil
		}
	}

	for i := range machines {
		for _, c := range *machines[i].Status.Conditions {
			if string(c.Type) == string(helper.KeepalivedMaster) &&
				string(c.Status) != string(helper.ConditionFalse) {
				return machines[i], nil
			}
		}
	}
	if len(machines) == 0 {
		return yawolv1beta1.LoadBalancerMachine{}, helper.ErrNoLBMFoundForScaleDown
	}
	return machines[0], nil
}

func (r *LoadBalancerSetReconciler) patchLoadBalancerSetStatus(
	ctx context.Context,
	lbs *yawolv1beta1.LoadBalancerSet,
	status yawolv1beta1.LoadBalancerSetStatus,
) error {
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return err
	}
	patch := []byte(`{"status":` + string(statusJSON) + `}`)
	return r.Client.Status().Patch(ctx, lbs, client.RawPatch(types.MergePatchType, patch))
}

// Decides whether the machine should be deleted or not
// True if created before 10 minutes and no condition added yet
// True if LastHeartbeatTime is > 5 minutes
// True if a condition is not good for 5 minutes
func shouldMachineBeDeleted(machine yawolv1beta1.LoadBalancerMachine) (bool, error) {
	before5Minutes := metav1.Time{Time: time.Now().Add(-5 * time.Minute)}
	before10Minutes := metav1.Time{Time: time.Now().Add(-10 * time.Minute)}

	// to handle the initial 10 minutes
	if machine.CreationTimestamp.Before(&before10Minutes) &&
		(machine.Status.Conditions == nil ||
			len(*machine.Status.Conditions) == 0) {
		return true, fmt.Errorf("no condition after 10 min: %w",
			helper.ErrNotAllConditionsSet,
		)
	}

	// As soon as a conditions are set
	if machine.Status.Conditions != nil {
		for _, condition := range *machine.Status.Conditions {
			if condition.LastHeartbeatTime.Before(&before5Minutes) {
				return true, fmt.Errorf(
					"no condition heartbeat in the last 5 min: %w",
					helper.ErrConditionsLastHeartbeatTimeToOld,
				)
			}

			if condition.LastTransitionTime.Before(&before5Minutes) {
				if helper.LoadBalancerSetConditionIsFalse(condition) {
					return true, fmt.Errorf(
						"condition: %v, reason: %v, status: %v, message: %v, lastTransitionTime: %v - %w",
						condition.Type, condition.Reason, condition.Status, condition.Message, condition.LastTransitionTime,
						helper.ErrConditionsNotInCorrectState,
					)
				}
			}
		}
	}

	return false, nil
}

// Decides whether the machine is ready or not
// False if not all conditions are set
// False if LastHeartbeatTime is older than 180sec
// False if ConfigReady, EnvoyReady or EnvoyUpToDate are false
func isMachineReady(machine yawolv1beta1.LoadBalancerMachine) bool {
	before180seconds := metav1.Time{Time: time.Now().Add(-180 * time.Second)}

	// not ready if no conditions are available
	if machine.Status.Conditions == nil || len(*machine.Status.Conditions) < 6 {
		return false
	}

	// As soon as a conditions are set
	if machine.Status.Conditions != nil {
		for _, condition := range *machine.Status.Conditions {
			if condition.LastHeartbeatTime.Before(&before180seconds) {
				return false
			}
			if helper.LoadBalancerSetConditionIsFalse(condition) {
				return false
			}
		}
	}

	return true
}

func isMachineMaster(machine yawolv1beta1.LoadBalancerMachine) bool {
	if machine.Status.Conditions != nil {
		for _, condition := range *machine.Status.Conditions {
			if condition.Type == corev1.NodeConditionType(helper.KeepalivedMaster) {
				return condition.Status == corev1.ConditionTrue
			}
		}
	}
	return false
}

func (r *LoadBalancerSetReconciler) createMachine(ctx context.Context, set *yawolv1beta1.LoadBalancerSet) error {
	machineLabels := r.getMachineLabelsFromSet(set)
	revision := set.Annotations[helper.RevisionAnnotation]
	machine := yawolv1beta1.LoadBalancerMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      set.Name + "-" + randomString(5),
			Namespace: set.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: set.APIVersion,
					Kind:       set.Kind,
					Name:       set.Name,
					UID:        set.UID,
				},
			},
			Labels: machineLabels,
			Annotations: map[string]string{
				helper.RevisionAnnotation: revision,
			},
		},
		Spec: set.Spec.Template.Spec,
	}
	r.Recorder.Event(&machine, "Normal", "Created", fmt.Sprintf("Created LoadBalancerMachine %s/%s", machine.Namespace, machine.Name))
	return r.Client.Create(ctx, &machine, &client.CreateOptions{})
}

func (r *LoadBalancerSetReconciler) deleteMachine(ctx context.Context, machine *yawolv1beta1.LoadBalancerMachine) error {
	return r.Client.Delete(ctx, machine)
}

func (r *LoadBalancerSetReconciler) deleteAllMachines(ctx context.Context, set *yawolv1beta1.LoadBalancerSet) error {
	var childMachines yawolv1beta1.LoadBalancerMachineList
	if err := r.List(ctx, &childMachines, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(set.Spec.Selector.MatchLabels),
		Namespace:     set.Namespace,
	}); err != nil {
		r.Log.Error(err, helper.ErrListingChildLBMs.Error())
		return err
	}

	for i := range childMachines.Items {
		if err := r.Client.Delete(ctx, &childMachines.Items[i]); err != nil {
			return err
		}
		r.Recorder.Event(set, corev1.EventTypeNormal, "Deleted", "Deleted loadBalancerMachine "+childMachines.Items[i].Name)
	}
	r.Log.Info("finished cleaning up old loadBalancerMachines")
	return nil
}

func (r *LoadBalancerSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancerSet{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
			RateLimiter:             r.RateLimiter,
		}).
		Complete(r)
}

func copyLabelMap(lbs map[string]string) map[string]string {
	targetMap := make(map[string]string)
	for key, value := range lbs {
		targetMap[key] = value
	}
	return targetMap
}

func (r *LoadBalancerSetReconciler) getMachineLabelsFromSet(set *yawolv1beta1.LoadBalancerSet) map[string]string {
	return copyLabelMap(set.Spec.Selector.MatchLabels)
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func randomString(length int) string {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err) // out of randomness, should never happen
	}
	return fmt.Sprintf("%x", buf)[:length]
}
