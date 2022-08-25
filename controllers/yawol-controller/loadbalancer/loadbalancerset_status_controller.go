package loadbalancer

import (
	"context"

	"github.com/stackitcloud/yawol/internal/helper"
	errors2 "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerSetStatusReconciler struct { //nolint:revive // naming from kubebuilder
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	WorkerCount int
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
func (r *LoadBalancerSetStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("LoadBalancerSet", req.NamespacedName)

	var loadBalancerSet yawolv1beta1.LoadBalancerSet
	if err := r.Client.Get(ctx, req.NamespacedName, &loadBalancerSet); err != nil {
		// If not found just add an info log and ignore error
		if errors2.IsNotFound(err) {
			r.Log.Info("LoadBalancerSet not found", "lbs", req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var lb *yawolv1beta1.LoadBalancer
	var err error

	lb, err = helper.GetLoadBalancerForLoadBalancerSet(ctx, r.Client, &loadBalancerSet)
	if err != nil {
		if errors2.IsNotFound(err) {
			r.Log.Info("could not find LoadBalancer for LoadBalancerSet", "lbs", req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if lb == nil {
		r.Log.Info("could not find LoadBalancer for LoadBalancerSet")
		return ctrl.Result{}, nil
	}

	// Not the current revision
	if lb.Annotations[RevisionAnnotation] != loadBalancerSet.Annotations[RevisionAnnotation] {
		return ctrl.Result{}, nil
	}

	// Patch replicas from lbs status to lb status
	if loadBalancerSet.Status.Replicas != nil &&
		(lb.Status.Replicas == nil || *lb.Status.Replicas != *loadBalancerSet.Status.Replicas) {
		if err := helper.PatchLBStatus(ctx, r.Client.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			Replicas: loadBalancerSet.Status.Replicas,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}

	// Patch ready replicas from lbs to lb status
	if loadBalancerSet.Status.ReadyReplicas != nil &&
		(lb.Status.ReadyReplicas == nil || *lb.Status.ReadyReplicas != *loadBalancerSet.Status.ReadyReplicas) {
		if err := helper.PatchLBStatus(ctx, r.Client.Status(), lb, yawolv1beta1.LoadBalancerStatus{
			ReadyReplicas: loadBalancerSet.Status.ReadyReplicas,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerSetStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancerSet{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
		}).
		Complete(r)
}
