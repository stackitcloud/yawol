// Package controllers contains reconcile logic for yawollet
package controllers

import (
	"context"
	"fmt"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"

	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
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
	err := r.Client.Get(ctx, req.NamespacedName, lb)
	if err != nil {
		return ctrl.Result{}, err
	}

	lbm := &yawolv1beta1.LoadBalancerMachine{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: r.LoadbalancerMachineName, Namespace: req.Namespace}, lbm)
	if err != nil {
		return ctrl.Result{}, err
	}

	// current snapshot
	oldSnapshot, err := r.EnvoyCache.GetSnapshot("lb-id")
	if err != nil {
		return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: unable to get current snapshot", err), lbm)
	}

	// create new snapshot
	changed, snapshot, err := helper.CreateEnvoyConfig(r.RecorderLB, &oldSnapshot, lb, r.ListenAddress)
	if err != nil {
		_ = helper.UpdateLBMConditions(ctx, r.Status(), lbm,
			helper.ConfigReady, helper.ConditionFalse, "EnvoyConfigurationFailed", "new snapshot cant create successful")
		return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: unable to get current snapshot", err), lbm)
	}

	// update envoy snapshot if changed
	if changed {
		if err = snapshot.Consistent(); err != nil {
			_ = helper.UpdateLBMConditions(ctx, r.Status(), lbm,
				helper.ConfigReady, helper.ConditionFalse, "EnvoyConfigurationFailed", "new snapshot is not Consistent")
			return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: new snapshot is not consistent", err), lbm)
		}

		err = r.EnvoyCache.SetSnapshot("lb-id", snapshot)
		if err != nil {
			_ = helper.UpdateLBMConditions(ctx, r.Status(), lbm,
				helper.ConfigReady, helper.ConditionFalse, "EnvoyConfigurationFailed", "new snapshot cant set to envoy envoycache")
			return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("%w: nable to set new snapshot", err), lbm)
		}
		err = helper.UpdateLBMConditions(ctx, r.Status(), lbm,
			helper.ConfigReady, helper.ConditionTrue, "EnvoyConfigurationUpToDate", "envoy config is successfully updated")
		if err != nil {
			return ctrl.Result{}, err
		}
		r.Log.Info("Envoy snapshot updated", "snapshot version", snapshot.GetVersion(resource.ClusterType))
		r.Recorder.Event(lbm,
			"Normal",
			"Update",
			fmt.Sprintf("Envoy is updated to new snapshot version: %v", snapshot.GetVersion(resource.ClusterType)))
	} else {
		err = helper.UpdateLBMConditions(ctx, r.Status(), lbm,
			helper.ConfigReady, helper.ConditionTrue, "EnvoyConfigurationCreated", "envoy config is already up to date")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// update envoy status condition
	err = helper.UpdateEnvoyStatus(ctx, r.Status(), lbm)
	if err != nil {
		return ctrl.Result{}, err
	}

	// check envoy snapshot and update condition
	if changed {
		err = helper.CheckEnvoyVersion(ctx, r.Status(), lbm, snapshot)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		err = helper.CheckEnvoyVersion(ctx, r.Status(), lbm, oldSnapshot)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// update keepalived status condition
	err = helper.UpdateKeepalivedStatus(ctx, r.Status(), r.KeepalivedStatsFile, lbm)
	if err != nil {
		return ctrl.Result{}, err
	}

	// update Metrics
	err = helper.WriteLBMMetrics(ctx, r.Status(), r.KeepalivedStatsFile, lbm)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Duration(r.RequeueTime) * time.Second}, nil
}

// SetupWithManager is used by kubebuilder to init the controller loop
func (r *LoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancer{}).
		Complete(r)
}
