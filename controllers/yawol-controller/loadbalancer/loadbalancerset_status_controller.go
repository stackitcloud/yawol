package loadbalancer

import (
	"context"
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerSetStatusReconciler struct {
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
		// Throw if error more serve than 404
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		r.Log.Info("LoadBalancerSet not found", "lbs", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if loadBalancerSet.OwnerReferences == nil {
		return ctrl.Result{}, nil
	}
	var lb yawolv1beta1.LoadBalancer
	for _, ref := range loadBalancerSet.OwnerReferences {
		if ref.Kind == LoadBalancerKind {
			if err := r.Client.Get(ctx, types.NamespacedName{
				Namespace: loadBalancerSet.Namespace,
				Name:      ref.Name,
			}, &lb); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return ctrl.Result{}, err
				}
				r.Log.Info("could not find LoadBalancer for LoadBalancerSet")
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
		}
	}

	// Not the current revision
	if lb.Annotations[RevisionAnnotation] != loadBalancerSet.Annotations[RevisionAnnotation] {
		return ctrl.Result{}, nil
	}

	// Patch replicas from lbs to lb
	if loadBalancerSet.Status.Replicas != nil &&
		(lb.Status.Replicas == nil || *lb.Status.Replicas != *loadBalancerSet.Status.Replicas) {
		if err := r.patchLBStatus(ctx, &lb, yawolv1beta1.LoadBalancerStatus{
			Replicas: loadBalancerSet.Status.Replicas,
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Patch ready replicas from lbs to lb
	if loadBalancerSet.Status.ReadyReplicas != nil &&
		(lb.Status.ReadyReplicas == nil || *lb.Status.ReadyReplicas != *loadBalancerSet.Status.ReadyReplicas) {
		if err := r.patchLBStatus(ctx, &lb, yawolv1beta1.LoadBalancerStatus{
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

func (r *LoadBalancerSetStatusReconciler) patchLBStatus(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	lbStatus yawolv1beta1.LoadBalancerStatus,
) error {
	lbStatusJson, _ := json.Marshal(lbStatus)
	patch := []byte(`{"status":` + string(lbStatusJson) + `}`)
	return r.Client.Status().Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}
