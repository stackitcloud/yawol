// Package controllers contains reconcile logic for yawollet
package controllers

import (
	"context"
	"fmt"
	"sort"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"

	envoycache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/go-logr/logr"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// LoadBalancerReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerReconciler struct {
	client.Client
	Log                     logr.Logger
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	RecorderLB              record.EventRecorder
	LoadbalancerName        string
	LoadbalancerMachineName string
	EnvoyCache              envoycache.SnapshotCache
	ListenAddress           string
	RequeueDuration         time.Duration
	KeepalivedStatsFile     string
}

// Reconcile handles reconciliation of loadbalancer object
func (r *LoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancer", req.NamespacedName)

	if req.Name != r.LoadbalancerName {
		return ctrl.Result{}, nil
	}

	lb := &yawolv1beta1.LoadBalancer{}
	if err := r.Client.Get(ctx, req.NamespacedName, lb); err != nil {
		return ctrl.Result{}, err
	}

	lbm := &yawolv1beta1.LoadBalancerMachine{}
	if err := r.Client.Get(
		ctx, client.ObjectKey{Name: r.LoadbalancerMachineName, Namespace: req.Namespace}, lbm,
	); err != nil {
		return ctrl.Result{}, err
	}

	conditionsMap := map[corev1.NodeConditionType]corev1.NodeCondition{}
	if lbm.Status.Conditions != nil {
		for _, v := range *lbm.Status.Conditions {
			conditionsMap[v.Type] = *v.DeepCopy()
		}
	}

	// this error will be returned later in order to always
	// keep metrics and conditions up-to-date
	reconcileError := r.reconcile(ctx, lb, lbm, conditionsMap)

	metrics, err := helper.GetMetrics(r.KeepalivedStatsFile)
	if err != nil {
		return ctrl.Result{}, err
	}

	// convert conditions back to slice for LoadBalancerMachine status
	var conditions []corev1.NodeCondition
	for _, con := range conditionsMap {
		conditions = append(conditions, con)
	}

	// keep conditions in stable order for convenience
	sort.Slice(conditions, func(i, j int) bool {
		return conditions[i].Type < conditions[j].Type
	})

	patch := client.MergeFrom(lbm.DeepCopy())
	lbm.Status.Conditions = &conditions
	lbm.Status.Metrics = &metrics

	if err := r.Client.Status().Patch(ctx, lbm, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: r.RequeueDuration}, reconcileError
}

func (r *LoadBalancerReconciler) reconcile(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	lbm *yawolv1beta1.LoadBalancerMachine,
	conditions map[corev1.NodeConditionType]corev1.NodeCondition,
) error {
	// enable ad hoc debugging if configured
	if err := helper.EnableAdHocDebugging(lb, lbm, r.Recorder, r.LoadbalancerMachineName); err != nil {
		return kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: unable to get current snapshot", err), lbm)
	}

	// current snapshot
	oldSnapshot, err := r.EnvoyCache.GetSnapshot("lb-id")
	if err != nil {
		return kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: unable to get current snapshot", err), lbm)
	}

	// create new snapshot
	changed, snapshot, err := helper.CreateEnvoyConfig(r.RecorderLB, oldSnapshot, lb, r.ListenAddress)
	if err != nil {
		helper.UpdateLBMConditions(
			conditions, helper.ConfigReady, helper.ConditionFalse, "EnvoyConfigurationFailed", "new snapshot cant create successful",
		)
		return kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: unable to create new snapshot", err), lbm)
	}

	// update envoy snapshot if changed
	if changed {
		if err := snapshot.Consistent(); err != nil {
			helper.UpdateLBMConditions(
				conditions, helper.ConfigReady, helper.ConditionFalse, "EnvoyConfigurationFailed", "new snapshot is not Consistent",
			)
			return kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: new snapshot is not consistent", err), lbm)
		}

		if err := r.EnvoyCache.SetSnapshot(ctx, "lb-id", &snapshot); err != nil {
			helper.UpdateLBMConditions(
				conditions, helper.ConfigReady, helper.ConditionFalse, "EnvoyConfigurationFailed", "new snapshot cant set to envoy envoycache",
			)
			return kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: unable to set new snapshot", err), lbm)
		}

		helper.UpdateLBMConditions(
			conditions, helper.ConfigReady, helper.ConditionTrue, "EnvoyConfigurationUpToDate", "envoy config is successfully updated",
		)

		r.Log.Info("Envoy snapshot updated", "snapshot version", snapshot.GetVersion(resource.ClusterType))
		r.Recorder.Event(lbm,
			"Normal",
			"Update",
			fmt.Sprintf("Envoy is updated to new snapshot version: %v", snapshot.GetVersion(resource.ClusterType)))
	} else {
		helper.UpdateLBMConditions(
			conditions, helper.ConfigReady, helper.ConditionTrue, "EnvoyConfigurationCreated", "envoy config is already up to date",
		)
	}

	// update envoy status condition
	helper.UpdateEnvoyStatus(conditions)

	// check envoy snapshot and update condition
	if changed {
		helper.CheckEnvoyVersion(conditions, &snapshot)
	} else {
		helper.CheckEnvoyVersion(conditions, oldSnapshot)
	}

	// update keepalived status condition
	helper.UpdateKeepalivedStatus(conditions, r.KeepalivedStatsFile)

	return nil
}

// SetupWithManager is used by kubebuilder to init the controller loop
func (r *LoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancer{}).
		WithEventFilter(predicate.And(
			predicate.NewPredicateFuncs(func(obj client.Object) bool { return obj.GetName() == r.LoadbalancerName }),
			predicate.Or(
				predicate.AnnotationChangedPredicate{},
				predicate.GenerationChangedPredicate{},
			),
		)).
		WithOptions(controller.Options{
			// Cap exponential backoff to the expected reconciliation frequency.
			// After an API Server outage this should ensure the status is
			// updated fast enough, before the LoadBalancerMachine is considered
			// stale/broken.
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](1*time.Second, r.RequeueDuration),
				&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		}).
		Complete(r)
}
