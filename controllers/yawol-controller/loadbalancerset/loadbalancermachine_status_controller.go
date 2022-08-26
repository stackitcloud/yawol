package loadbalancerset

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerMachineReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerMachineStatusReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	WorkerCount int
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
func (r *LoadBalancerMachineStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("LoadBalancerMachineStatus", req.NamespacedName)

	var loadBalancerMachine yawolv1beta1.LoadBalancerMachine
	if err := r.Client.Get(ctx, req.NamespacedName, &loadBalancerMachine); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		r.Log.Info("error retrieving LoadBalancerMachine", "lbm", req)
		return ctrl.Result{}, err
	}

	if shouldMachineBeDeleted(loadBalancerMachine) {
		if err := r.Client.Delete(ctx, &loadBalancerMachine); err != nil {
			return ctrl.Result{}, err
		}
		r.Log.Info("Deleted LoadBalancerMachine", "lbm", req)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *LoadBalancerMachineStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancerMachine{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.WorkerCount,
		}).
		Complete(r)
}
