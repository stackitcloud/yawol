package control_controller

import (
	"context"
	"errors"
	"strings"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/target_controller"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EventSource is the name of the eventsource that get forwarded to the service of the customer
const EventSource = "yawol-service"

// EventReconciler reconciles service Objects with type Event
type EventReconciler struct {
	TargetClient  client.Client
	ControlClient client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
}

// +kubebuilder:rbac:groups=core,resources=node,verbs=get;list;watch
func (r *EventReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("event", req.NamespacedName)

	var event coreV1.Event
	if err := r.ControlClient.Get(ctx, req.NamespacedName, &event); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// skip no event for forwarding
	if event.Source.Component != EventSource || event.InvolvedObject.Kind != "LoadBalancer" {
		return ctrl.Result{}, nil
	}

	// get lb for event
	var lb yawolv1beta1.LoadBalancer
	if err := r.ControlClient.Get(ctx, client.ObjectKey{
		Name:      event.InvolvedObject.Name,
		Namespace: event.InvolvedObject.Namespace,
	}, &lb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// get svc from target cluster
	svc := coreV1.Service{}
	serviceParams := strings.Split(lb.Annotations[target_controller.ServiceAnnotation], "/")
	if len(serviceParams) != 2 {
		return ctrl.Result{}, errors.New("could not read service namespacedname from annotation")
	}
	if err := r.TargetClient.Get(ctx, client.ObjectKey{Name: serviceParams[1], Namespace: serviceParams[0]}, &svc); err != nil {
		return ctrl.Result{}, err
	}

	// forward event
	r.Recorder.Event(&svc, event.Type, event.Reason, event.Message)

	return ctrl.Result{}, nil
}

func (r *EventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&coreV1.Event{}).
		Complete(r)
}
