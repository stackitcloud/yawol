package loadbalancerset

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
)

// LBMStatusReconciler watches LoadBalancerMachines and determines if a machine
// should be deleted. A machine should be deleted if its conditions are either
// stale, or have been in a not-healthy state for some time.
//
// Deletion is not performed immediately, instead a new Condition is added, and
// after `DeletionGracePeriod` has passed (and the machine is still not ready),
// the object is actually deleted. This ensures that after an e.g. API Server
// (where both yawollet and the yawol-controllers cannot update the status), the
// yawollet is given a chance to update the heartbeat.
type LBMStatusReconciler struct {
	client.Client
	WorkerCount int
	RateLimiter ratelimiter.RateLimiter

	DeletionGracePeriod time.Duration
}

func (r *LBMStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	loadBalancerMachine := &yawolv1beta1.LoadBalancerMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, loadBalancerMachine); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("error retrieving LoadBalancerMachine: %w", err)
	}

	if loadBalancerMachine.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if condition := findDeletionCondition(loadBalancerMachine); condition != nil && condition.Status == corev1.ConditionTrue {
		if err := r.handleMarkedMachine(ctx, log, loadBalancerMachine, condition); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	if shouldBeDeleted, reason := shouldMachineBeDeleted(loadBalancerMachine); shouldBeDeleted {
		if err := r.markForDeletion(ctx, loadBalancerMachine, reason); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("LoadBalancerMachine failed ReadyChecks and is marked for deletion", "Reason:", reason)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *LBMStatusReconciler) handleMarkedMachine(
	ctx context.Context,
	log logr.Logger,
	machine *yawolv1beta1.LoadBalancerMachine,
	condition *corev1.NodeCondition,
) error {
	if shouldBeDeleted, _ := shouldMachineBeDeleted(machine); !shouldBeDeleted {
		removeDeletionCondition(machine)
		if err := r.Update(ctx, machine); err != nil {
			return err
		}
		log.Info("Reset pending deletion, since machine no longer should be deleted")
		return nil
	}

	gracePeriodEnd := &metav1.Time{Time: time.Now().Add(-r.DeletionGracePeriod)}
	if condition.LastTransitionTime.Before(gracePeriodEnd) {
		log.Info("Grace period ended, deleting machine")
		return r.Delete(ctx, machine)
	}

	return nil
}

func (r *LBMStatusReconciler) markForDeletion(ctx context.Context, machine *yawolv1beta1.LoadBalancerMachine, message string) error {
	setDeletionCondition(machine, corev1.NodeCondition{
		Status:  corev1.ConditionTrue,
		Reason:  "PendingDeletion",
		Message: message,
	})
	return r.Update(ctx, machine)
}

func (r *LBMStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancerMachine{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
			RateLimiter:             r.RateLimiter,
		}).
		Complete(r)
}
