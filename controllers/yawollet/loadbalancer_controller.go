// Package controllers contains reconcile logic for yawollet
package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"

	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	envoycache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
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
	RequeueTime             int
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

	conditions := map[corev1.NodeConditionType]corev1.NodeCondition{}
	if lbm.Status.Conditions != nil {
		for _, v := range *lbm.Status.Conditions {
			conditions[v.Type] = *v.DeepCopy()
		}
	}

	err := r.reconcile(ctx, lb, lbm, conditions)

	metricString, err := helper.GetMetricString(r.KeepalivedStatsFile)
	if err != nil {
		return ctrl.Result{}, err
	}

	conditionString, err := helper.GetLBMConditionString(conditions)
	if err != nil {
		return ctrl.Result{}, err
	}

	patch := []byte(`{ "status": { "conditions": ` + conditionString + `, "metrics": ` + metricString + `}}`)
	if err := r.Client.Status().Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Duration(r.RequeueTime) * time.Second}, err
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
		WithEventFilter(yawolletPredicate(r.LoadbalancerName)).
		Complete(r)
}

func yawolletPredicate(loadbalancerName string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			return event.Object.GetName() == loadbalancerName
		},
		DeleteFunc: func(event event.DeleteEvent) bool {
			return false
		},
		UpdateFunc: func(event event.UpdateEvent) bool {
			if event.ObjectNew.GetName() != loadbalancerName {
				return false
			}
			return !reflect.DeepEqual(
				event.ObjectOld.(*yawolv1beta1.LoadBalancer).Spec,
				event.ObjectNew.(*yawolv1beta1.LoadBalancer).Spec)
		},
		GenericFunc: func(event event.GenericEvent) bool {
			return false
		},
	}
}
