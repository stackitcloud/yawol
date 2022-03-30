package control_controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/target_controller"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerReconciler reconciles service Objects with type LoadBalancer
type LoadBalancerReconciler struct {
	TargetClient  client.Client
	ControlClient client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
func (r *LoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancer", req.NamespacedName)

	var lb yawolv1beta1.LoadBalancer
	if err := r.ControlClient.Get(ctx, req.NamespacedName, &lb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	namespacedName := types.NamespacedName{}
	if service := strings.Split(lb.Annotations[target_controller.ServiceAnnotation], "/"); len(service) == 2 {
		namespacedName.Namespace = service[0]
		namespacedName.Name = service[1]
	} else {
		return ctrl.Result{}, errors.New("could not read service namespacedname from annotation")
	}

	var svc v1.Service
	if err := r.TargetClient.Get(ctx, namespacedName, &svc); err != nil {
		r.Log.Error(err, "could not retrieve svc")
		return ctrl.Result{}, err
	}

	// update externalIP in service if lb has ready replicas
	if lb.Status.ExternalIP != nil && lb.Status.ReadyReplicas != nil && *lb.Status.ReadyReplicas > 0 {
		loadBalancerStatus := v1.LoadBalancerStatus{
			Ingress: []v1.LoadBalancerIngress{
				{
					IP: *lb.Status.ExternalIP,
				},
			}}

		if !reflect.DeepEqual(loadBalancerStatus, svc.Status.LoadBalancer) {
			svc.Status.LoadBalancer = loadBalancerStatus
			if err := r.patchStatusOfService(ctx, &svc); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(&svc,
				v1.EventTypeNormal,
				"creation",
				fmt.Sprintf("LoadBalancer is successfully created with IP %v", *lb.Status.ExternalIP))
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *LoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&yawolv1beta1.LoadBalancer{}).
		Complete(r)
}

func (r *LoadBalancerReconciler) patchStatusOfService(ctx context.Context, svc *v1.Service) error {
	return r.TargetClient.Status().Update(ctx, svc, &client.UpdateOptions{})
}
