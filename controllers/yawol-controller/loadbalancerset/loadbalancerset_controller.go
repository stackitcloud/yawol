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
	v12 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
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

	readyMachines := make([]yawolv1beta1.LoadBalancerMachine, 0)
	notReadyMachines := make([]yawolv1beta1.LoadBalancerMachine, 0)
	deletedMachines := make([]yawolv1beta1.LoadBalancerMachine, 0)
	for i := range childMachines.Items {
		if childMachines.Items[i].DeletionTimestamp != nil {
			deletedMachines = append(deletedMachines, childMachines.Items[i])
		} else if isMachineReady(childMachines.Items[i]) {
			readyMachines = append(readyMachines, childMachines.Items[i])
		} else {
			notReadyMachines = append(notReadyMachines, childMachines.Items[i])
		}
	}

	if res, err := r.reconcileStatus(ctx, &set, readyMachines); err != nil || res.Requeue || res.RequeueAfter != 0 {
		return res, err
	}

	if res, err := r.reconcileReplicas(
		ctx,
		&set,
		deletedMachines,
		notReadyMachines,
		readyMachines,
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

	// remove finalizer
	kubernetes.RemoveFinalizerIfNeeded(ctx, r.Client, set, FINALIZER)

	helper.RemoveLoadBalancerSetMetrics(
		*set,
		r.Metrics,
	)

	// stop reconciliation as item is being deleted
	return ctrl.Result{}, nil
}

func (r *LoadBalancerSetReconciler) reconcileStatus(
	ctx context.Context,
	set *yawolv1beta1.LoadBalancerSet,
	readyMachines []yawolv1beta1.LoadBalancerMachine,
) (ctrl.Result, error) {
	// Write replicas into status
	if set.Status.Replicas == nil || *set.Status.Replicas != set.Spec.Replicas {
		replicas := set.Spec.Replicas
		if err := r.patchLoadBalancerSetStatus(ctx, set, yawolv1beta1.LoadBalancerSetStatus{
			Replicas: &replicas,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Write ready replicas into status
	if set.Status.ReadyReplicas == nil || *set.Status.ReadyReplicas != len(readyMachines) {
		replicas := len(readyMachines)
		if err := r.patchLoadBalancerSetStatus(ctx, set, yawolv1beta1.LoadBalancerSetStatus{
			ReadyReplicas: &replicas,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerSetReconciler) reconcileReplicas(
	ctx context.Context,
	set *yawolv1beta1.LoadBalancerSet,
	deletedMachines, notReadyMachines, readyMachines []yawolv1beta1.LoadBalancerMachine,
) (ctrl.Result, error) {
	var currentReplicas, desiredReplicas, deletedReplicas int
	desiredReplicas = set.Spec.Replicas
	currentReplicas = len(deletedMachines) + len(notReadyMachines) + len(readyMachines)
	deletedReplicas = len(deletedMachines)

	if currentReplicas < desiredReplicas {
		if err := r.createMachine(ctx, set); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 2}, nil
	}

	if desiredReplicas < currentReplicas-deletedReplicas {
		// Delete not ready machines first
		if len(notReadyMachines) > 0 {
			if err := r.deleteMachine(ctx, &notReadyMachines[0]); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second * 2}, nil
		}

		// Delete ready machines
		if len(readyMachines) > 0 {
			if err := r.deleteMachine(ctx, &readyMachines[0]); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second * 2}, nil
		}
	}

	return ctrl.Result{}, nil
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
func shouldMachineBeDeleted(machine yawolv1beta1.LoadBalancerMachine) bool {
	before5Minutes := v1.Time{Time: time.Now().Add(-5 * time.Minute)}
	before10Minutes := v1.Time{Time: time.Now().Add(-10 * time.Minute)}

	// to handle the initial 10 minutes
	if machine.CreationTimestamp.Before(&before10Minutes) &&
		(machine.Status.Conditions == nil ||
			len(*machine.Status.Conditions) == 0) {
		return true
	}

	// As soon as a conditions are set
	if machine.Status.Conditions != nil {
		for _, condition := range *machine.Status.Conditions {
			if condition.LastHeartbeatTime.Before(&before5Minutes) {
				return true
			}

			if condition.LastTransitionTime.Before(&before5Minutes) {
				if helper.LoadBalancerSetConditionIsFalse(condition) {
					return true
				}
			}
		}
	}

	return false
}

// Decides whether the machine is ready or not
// False if not all conditions are set
// False if LastHeartbeatTime is older than 60sec
// False if ConfigReady, EnvoyReady or EnvoyUpToDate are false
func isMachineReady(machine yawolv1beta1.LoadBalancerMachine) bool {
	before60seconds := v1.Time{Time: time.Now().Add(-60 * time.Second)}

	// not ready if no conditions are available
	if machine.Status.Conditions == nil || len(*machine.Status.Conditions) < 3 {
		return false
	}

	// As soon as a conditions are set
	if machine.Status.Conditions != nil {
		for _, condition := range *machine.Status.Conditions {
			if condition.LastHeartbeatTime.Before(&before60seconds) {
				return false
			}
			if helper.LoadBalancerSetConditionIsFalse(condition) {
				return false
			}
		}
	}

	return true
}

func (r *LoadBalancerSetReconciler) createMachine(ctx context.Context, set *yawolv1beta1.LoadBalancerSet) error {
	machineLabels := r.getMachineLabelsFromSet(set)
	machine := yawolv1beta1.LoadBalancerMachine{
		ObjectMeta: v1.ObjectMeta{
			Name:      set.Name + "-" + randomString(5),
			Namespace: set.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: set.APIVersion,
					Kind:       set.Kind,
					Name:       set.Name,
					UID:        set.UID,
				},
			},
			Labels: machineLabels,
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
		r.Recorder.Event(set, v12.EventTypeNormal, "Deleted", "Deleted loadBalancerMachine "+childMachines.Items[i].Name)
	}
	r.Log.Info("finished cleaning up old loadBalancerMachines")
	return nil
}

func (r *LoadBalancerSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancerSet{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
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
