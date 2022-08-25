package targetcontroller

import (
	"context"
	"crypto/sha256"

	"encoding/base32"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	"github.com/stackitcloud/yawol/internal/helper/kubernetes"

	"github.com/go-logr/logr"
	coreV1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ServiceFinalizer      = "stackit.cloud/loadbalancer"
	ServiceAnnotation     = "yawol.stackit.cloud/serviceName"
	LoadBalancerLabelName = "yawol.stackit.cloud/loadbalancer"
)

// ServiceReconciler reconciles service Objects with type LoadBalancer
type ServiceReconciler struct {
	TargetClient           client.Client
	ControlClient          client.Client
	InfrastructureDefaults InfrastructureDefaults
	Log                    logr.Logger
	Scheme                 *runtime.Scheme
	Recorder               record.EventRecorder
	ClassName              string
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("service", req.NamespacedName)

	svc := &coreV1.Service{}
	if err := r.TargetClient.Get(ctx, req.NamespacedName, svc); err != nil {
		// If not found just add an info log and ignore error
		if apierrors.IsNotFound(err) {
			r.Log.Info("svc could not be found", "svc", req)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	className, ok := svc.Annotations[yawolv1beta1.ServiceClassName]
	if !ok {
		className = ""
	}

	infraDefaults := GetMergedInfrastructureDetails(r.InfrastructureDefaults, svc)

	if className != r.ClassName {
		r.Log.WithValues("service", req.NamespacedName).Info("service and controller classname does not match")
		if err := r.ControlClient.Get(ctx, types.NamespacedName{
			Namespace: *infraDefaults.Namespace,
			Name:      req.Namespace + "--" + req.Name,
		}, &yawolv1beta1.LoadBalancer{}); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		// only trigger deletion routine if the lb still exists
		r.Log.WithValues("service", req.NamespacedName).Info("trigger deletion routine")
		return r.deletionRoutine(ctx, svc, infraDefaults)
	}

	if svc.Spec.Type != coreV1.ServiceTypeLoadBalancer {
		r.Log.WithValues("service", req.NamespacedName).Info("service is not of type LoadBalancer, trigger deletion routine")
		return r.deletionRoutine(ctx, svc, infraDefaults)
	}

	if svc.DeletionTimestamp != nil {
		return r.deletionRoutine(ctx, svc, infraDefaults)
	}

	var err error

	err = kubernetes.AddFinalizerIfNeeded(ctx, r.TargetClient, svc, ServiceFinalizer)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err = helper.ValidateService(svc); err != nil {
		return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, fmt.Errorf("validation failed: %v", err), svc)
	}

	loadBalancer := &yawolv1beta1.LoadBalancer{}

	err = r.ControlClient.Get(ctx, types.NamespacedName{
		Namespace: *infraDefaults.Namespace,
		Name:      req.Namespace + "--" + req.Name,
	}, loadBalancer)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	// not found create loadbalancer
	if err != nil && apierrors.IsNotFound(err) {
		if svc.DeletionTimestamp != nil {
			return ctrl.Result{}, kubernetes.RemoveFinalizerIfNeeded(ctx, r.TargetClient, svc, ServiceFinalizer)
		}

		if err = r.createLoadBalancer(ctx, req.NamespacedName, svc, infraDefaults); err != nil {
			return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, err, svc)
		}
		r.Recorder.Event(svc, coreV1.EventTypeNormal, "creation", "LoadBalancer is in creation")

		return ctrl.Result{Requeue: true}, nil
	}

	if loadBalancer.ObjectMeta.Annotations[ServiceAnnotation] != svc.Namespace+"/"+svc.Name {
		if err := r.addAnnotation(ctx, loadBalancer, ServiceAnnotation, svc.Namespace+"/"+svc.Name); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, err
	}

	// if port specs differ, patch svc => lb
	err = r.reconcilePorts(ctx, loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	// if replicas differ, patch svc => lb
	err = r.reconcileReplicas(ctx, loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	// if endpoints differ to ready nodes, patch node => endpoints
	err = r.reconcileNodes(ctx, loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileInfrastructure(ctx, loadBalancer, infraDefaults)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileDebugSettings(ctx, loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileOptions(ctx, loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = helper.CheckExistingFloatingIPChanged(svc)
	if err != nil {
		return ctrl.Result{}, kubernetes.SendErrorAsEvent(r.Recorder, err, svc)
	}

	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&coreV1.Service{}).
		Complete(r)
}

func (r *ServiceReconciler) createLoadBalancer(
	ctx context.Context,
	namespacedName types.NamespacedName,
	svc *coreV1.Service,
	infraConfig InfrastructureDefaults,
) error {
	hash := sha256.Sum256([]byte(*infraConfig.Namespace + "." + namespacedName.Namespace + "--" + namespacedName.Name))
	hashstring := strings.ToLower(base32.StdEncoding.EncodeToString(hash[:]))[:16]
	lbNN := getLoadBalancerNamespacedName(&infraConfig, svc)
	loadBalancer := yawolv1beta1.LoadBalancer{
		ObjectMeta: v1.ObjectMeta{
			Namespace: lbNN.Namespace,
			Name:      lbNN.Name,
			Annotations: map[string]string{
				ServiceAnnotation: namespacedName.String(),
			},
		},
		Spec: yawolv1beta1.LoadBalancerSpec{
			Replicas: helper.GetReplicasFromService(svc),
			Selector: v1.LabelSelector{
				MatchLabels: map[string]string{
					LoadBalancerLabelName: hashstring,
				},
			},
			ExistingFloatingIP: helper.GetExistingFloatingIPFromAnnotation(svc),
			Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{
				FloatingNetID:    infraConfig.FloatingNetworkID,
				NetworkID:        *infraConfig.NetworkID,
				Flavor:           infraConfig.FlavorRef,
				Image:            infraConfig.ImageRef,
				AvailabilityZone: *infraConfig.AvailabilityZone,
				AuthSecretRef: coreV1.SecretReference{
					Name:      *infraConfig.AuthSecretName,
					Namespace: *infraConfig.Namespace,
				},
			},
			DebugSettings: helper.GetDebugSettings(svc),
			Options:       helper.GetOptions(svc),
		},
		Status: yawolv1beta1.LoadBalancerStatus{},
	}
	return r.ControlClient.Create(ctx, &loadBalancer, &client.CreateOptions{})
}

func (r *ServiceReconciler) reconcileInfrastructure(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	infraConfig InfrastructureDefaults,
) error {
	newInfra := yawolv1beta1.LoadBalancerInfrastructure{
		FloatingNetID:    infraConfig.FloatingNetworkID,
		NetworkID:        *infraConfig.NetworkID,
		Flavor:           infraConfig.FlavorRef,
		Image:            infraConfig.ImageRef,
		AvailabilityZone: *infraConfig.AvailabilityZone,
		AuthSecretRef: coreV1.SecretReference{
			Name:      *infraConfig.AuthSecretName,
			Namespace: *infraConfig.Namespace,
		},
	}
	if !reflect.DeepEqual(newInfra, lb.Spec.Infrastructure) {
		newInfraJSON, err := json.Marshal(newInfra)
		if err != nil {
			return err
		}
		patch := []byte(`{"spec":{"infrastructure":` + string(newInfraJSON) + `}}`)
		err = r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return err
		}
	}

	if infraConfig.InternalLB != nil && *infraConfig.InternalLB != lb.Spec.Options.InternalLB {
		patch := []byte(`{"spec":{"options":{"internalLB":` + strconv.FormatBool(*infraConfig.InternalLB) + `}}}`)
		err := r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ServiceReconciler) reconcilePorts(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc *coreV1.Service,
) error {
	if !reflect.DeepEqual(lb.Spec.Ports, svc.Spec.Ports) {
		if err := r.patchLoadBalancerPorts(ctx, lb, svc.Spec.Ports); err != nil {
			r.Log.WithValues("service", svc.Namespace).Error(err, "could not patch loadbalancer.spec.ports")
			return err
		}
		r.Recorder.Event(svc, coreV1.EventTypeNormal, "update", "LoadBalancer ports successfully synced with service ports")
	}
	return nil
}

func (r *ServiceReconciler) reconcileReplicas(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc *coreV1.Service,
) error {
	replicas := helper.GetReplicasFromService(svc)
	if replicas != lb.Spec.Replicas {
		if err := r.patchLoadBalancerReplicas(ctx, lb, replicas); err != nil {
			r.Log.WithValues("service", svc.Name).Error(err, "could not patch loadbalancer.spec.replicas")
			return err
		}
		r.Recorder.Event(svc, coreV1.EventTypeNormal, "update", "LoadBalancer replicas successfully synced with service replicas")
	}
	return nil
}

func (r *ServiceReconciler) reconcileNodes(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc *coreV1.Service,
) error {
	var nodes coreV1.NodeList
	if err := r.TargetClient.List(ctx, &nodes, &client.ListOptions{}); err != nil {
		return err
	}
	nodeEPs := getReadyEndpointsFromNodes(nodes.Items, svc.Spec.IPFamilies)

	if !EqualLoadBalancerEndpoints(lb.Spec.Endpoints, nodeEPs) {
		if err := r.patchLoadBalancerEndpoints(ctx, lb, nodeEPs); err != nil {
			return err
		}
		r.Recorder.Event(svc, coreV1.EventTypeNormal, "update", "LoadBalancer endpoints successfully synced with nodes addresses")
	}
	return nil
}

func (r *ServiceReconciler) reconcileOptions(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc *coreV1.Service,
) error {
	newOptions := helper.GetOptions(svc)
	if !reflect.DeepEqual(newOptions.LoadBalancerSourceRanges, lb.Spec.Options.LoadBalancerSourceRanges) {
		if err := r.patchLoadBalancerSourceRanges(ctx, lb, newOptions.LoadBalancerSourceRanges); err != nil {
			r.Log.WithValues("service", svc.Name).Error(err, "could not patch loadbalancer.spec.options.LoadBalancerSourceRanges")
			return err
		}
		r.Recorder.Event(svc, coreV1.EventTypeNormal, "update", "LoadBalancer SourceRanges successfully synced with service SourceRange")
	}

	// InternalLB is being reconciled by reconcileInfrastructure func

	if newOptions.TCPProxyProtocol != lb.Spec.Options.TCPProxyProtocol {
		patch := []byte(`{"spec":{"options":{"tcpProxyProtocol":` + strconv.FormatBool(newOptions.TCPProxyProtocol) + `}}}`)
		err := r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return err
		}
	}
	if !reflect.DeepEqual(newOptions.TCPProxyProtocolPortsFilter, lb.Spec.Options.TCPProxyProtocolPortsFilter) {
		data, err := json.Marshal(newOptions.TCPProxyProtocolPortsFilter)
		if err != nil {
			return err
		}
		patch := []byte(`{"spec":{"options":{"tcpProxyProtocolPortFilter":` + string(data) + `}}}`)
		return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
	}
	return nil
}

func (r *ServiceReconciler) reconcileDebugSettings(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc *coreV1.Service,
) error {
	newDebugSettings := helper.GetDebugSettings(svc)
	if !reflect.DeepEqual(newDebugSettings, lb.Spec.DebugSettings) {
		newDebugSettingsJSON, err := json.Marshal(newDebugSettings)
		if err != nil {
			return err
		}
		patch := []byte(`{"spec":{"debugSettings":` + string(newDebugSettingsJSON) + `}}`)
		return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
	}
	return nil
}

func getLoadBalancerNamespacedName(infraConfig *InfrastructureDefaults, svc *coreV1.Service) types.NamespacedName {
	return types.NamespacedName{
		Namespace: *infraConfig.Namespace,
		Name:      helper.GetLoadBalancerNameFromService(svc),
	}
}

func (r *ServiceReconciler) patchLoadBalancerPorts(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svcPorts []coreV1.ServicePort,
) error {
	svcPortsJSON, err := json.Marshal(svcPorts)
	if err != nil {
		return err
	}
	patch := []byte(`{"spec":{"ports":` + string(svcPortsJSON) + `}}`)

	return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *ServiceReconciler) patchLoadBalancerSourceRanges(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svcRanges []string,
) error {
	data, err := json.Marshal(svcRanges)
	if err != nil {
		return err
	}
	patch := []byte(`{"spec":{"options":{"loadBalancerSourceRanges":` + string(data) + `}}}`)

	return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *ServiceReconciler) patchLoadBalancerReplicas(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	replicas int,
) error {
	patch := []byte(`{"spec":{"replicas":` + fmt.Sprint(replicas) + `}}`)

	return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *ServiceReconciler) patchLoadBalancerEndpoints(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	eps []yawolv1beta1.LoadBalancerEndpoint,
) error {
	endpointsJSON, err := json.Marshal(eps)
	if err != nil {
		return err
	}
	patch := []byte(`{"spec":{"endpoints":` + string(endpointsJSON) + `}}`)

	return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}

func EqualLoadBalancerEndpoints(eps1, eps2 []yawolv1beta1.LoadBalancerEndpoint) bool {
	sort.Slice(eps1, func(i, j int) bool {
		return eps1[i].Name < eps1[j].Name
	})
	sort.Slice(eps2, func(i, j int) bool {
		return eps2[i].Name < eps2[j].Name
	})
	return reflect.DeepEqual(eps1, eps2)
}

func (r *ServiceReconciler) removeIngressIPFromStatus(
	ctx context.Context,
	svc *coreV1.Service,
) error {
	patch := []byte(`[{"op":"remove", "path":"/status/loadBalancer/ingress"}]`)
	return r.TargetClient.Status().Patch(ctx, svc, client.RawPatch(types.JSONPatchType, patch))
}

func (r *ServiceReconciler) deletionRoutine(
	ctx context.Context,
	svc *coreV1.Service,
	infraDefaults InfrastructureDefaults,
) (ctrl.Result, error) {
	var loadBalancer yawolv1beta1.LoadBalancer
	var err error
	if err = r.ControlClient.Get(
		ctx,
		getLoadBalancerNamespacedName(&infraDefaults, svc),
		&loadBalancer,
	); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if apierrors.IsNotFound(err) {
		className, ok := svc.Annotations[yawolv1beta1.ServiceClassName]
		if !ok {
			className = ""
		}
		if r.ClassName != className {
			return ctrl.Result{}, nil
		}

		if len(svc.Status.LoadBalancer.Ingress) != 0 {
			if err := r.removeIngressIPFromStatus(ctx, svc); err != nil {
				return ctrl.Result{}, err
			}
			r.Log.Info("successfully deleted loadbalancer ip on svc status")
		}
		r.Log.Info("load balancer deleted")
		return ctrl.Result{}, kubernetes.RemoveFinalizerIfNeeded(ctx, r.TargetClient, svc, ServiceFinalizer)
	}

	if loadBalancer.DeletionTimestamp == nil {
		if err := r.ControlClient.Delete(ctx, &loadBalancer, &client.DeleteOptions{}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *ServiceReconciler) addAnnotation(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	key, value string,
) error {
	keyJSON, err := json.Marshal(key)
	if err != nil {
		return err
	}
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return err
	}
	patch := []byte(`{"metadata": {"annotations": {` + string(keyJSON) + `: ` + string(valueJSON) + `}}}`)
	return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
}
