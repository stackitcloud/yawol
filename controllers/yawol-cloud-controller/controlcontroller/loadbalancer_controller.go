package controlcontroller

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/targetcontroller"
	"github.com/stackitcloud/yawol/internal/helper"
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

// Reconcile reconciles the LoadBalancerObject to patch the status in the Service
func (r *LoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("loadbalancer", req.NamespacedName)

	lb := &yawolv1beta1.LoadBalancer{}
	if err := r.ControlClient.Get(ctx, req.NamespacedName, lb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	namespacedName := types.NamespacedName{}
	if service := strings.Split(lb.Annotations[targetcontroller.ServiceAnnotation], "/"); len(service) == 2 {
		namespacedName.Namespace = service[0]
		namespacedName.Name = service[1]
	} else {
		return ctrl.Result{}, helper.ErrCouldNotReadSvcNameSpacedNameFromAnno
	}

	svc := &v1.Service{}
	if err := r.TargetClient.Get(ctx, namespacedName, svc); err != nil {
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
			err := helper.PatchServiceStatus(ctx, r.TargetClient.Status(), svc, &v1.ServiceStatus{LoadBalancer: loadBalancerStatus})
			if err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(svc,
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
