package target_controller

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

	yawolv1beta1 "dev.azure.com/schwarzit/schwarzit.ske/yawol.git/api/v1beta1"
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
// nolint: gocyclo // reconcile is always a little complex
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("service", req.NamespacedName)

	var svc coreV1.Service
	if err := r.TargetClient.Get(ctx, req.NamespacedName, &svc); err != nil {
		r.Log.Info("svc could not be found", "svc", req)
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
	} else if !hasFinalizer(svc.ObjectMeta, ServiceFinalizer) {
		if err := r.addFinalizer(ctx, svc, ServiceFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if err := validateService(svc); err != nil {
		r.Recorder.Event(&svc, coreV1.EventTypeWarning, "validationFailed", fmt.Sprintf("Validation failed: %v", err))
		return ctrl.Result{}, nil
	}

	var loadBalancer yawolv1beta1.LoadBalancer
	err := r.ControlClient.Get(ctx, types.NamespacedName{
		Namespace: *infraDefaults.Namespace,
		Name:      req.Namespace + "--" + req.Name,
	}, &loadBalancer)

	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if err != nil && apierrors.IsNotFound(err) {
		if svc.DeletionTimestamp == nil {
			if err = r.createLoadBalancer(ctx, req.NamespacedName, &svc, infraDefaults); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(&svc, coreV1.EventTypeNormal, "creation", "LoadBalancer is in creation")
		} else if hasFinalizer(svc.ObjectMeta, ServiceFinalizer) {
			if err = r.removeFinalizer(ctx, svc, ServiceFinalizer); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if !hasAnnotation(loadBalancer.ObjectMeta, ServiceAnnotation, svc.Namespace+"/"+svc.Name) {
		if err = r.addAnnotation(ctx, loadBalancer, ServiceAnnotation, svc.Namespace+"/"+svc.Name); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, err
	}

	// if port specs differ, patch svc => lb
	if !reflect.DeepEqual(loadBalancer.Spec.Ports, svc.Spec.Ports) {
		if err = r.patchLoadBalancerPorts(ctx, loadBalancer, svc.Spec.Ports); err != nil {
			r.Log.WithValues("service", req.NamespacedName).Error(err, "could not patch loadbalancer.spec.ports")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(&svc, coreV1.EventTypeNormal, "update", "LoadBalancer ports successfully synced with service ports")
		return ctrl.Result{Requeue: true}, nil
	}

	// if source IP ranges differ, patch svc => lb
	if !reflect.DeepEqual(loadBalancer.Spec.LoadBalancerSourceRanges, svc.Spec.LoadBalancerSourceRanges) {
		if err = r.patchLoadBalancerSourceRanges(ctx, loadBalancer, svc.Spec.LoadBalancerSourceRanges); err != nil {
			r.Log.WithValues("service", req.NamespacedName).Error(err, "could not patch loadbalancer.spec.LoadBalancerSourceRanges")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(&svc, coreV1.EventTypeNormal, "update", "LoadBalancer SourceRanges successfully synced with service SourceRange")
		return ctrl.Result{Requeue: true}, nil
	}

	// if replicas differ, patch svc => lb
	replicas := r.getReplicas(&svc)
	if replicas != loadBalancer.Spec.Replicas {
		if err = r.patchLoadBalancerReplicas(ctx, loadBalancer, replicas); err != nil {
			r.Log.WithValues("service", req.NamespacedName).Error(err, "could not patch loadbalancer.spec.replicas")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(&svc, coreV1.EventTypeNormal, "update", "LoadBalancer replicas successfully synced with service replicas")
		return ctrl.Result{Requeue: true}, nil
	}

	// if endpoints differ to ready nodes, patch node => endpoints
	var nodes coreV1.NodeList
	if err = r.TargetClient.List(ctx, &nodes, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, err
	}

	nodeEPs := getReadyEndpointsFromNodes(nodes.Items, svc.Spec.IPFamilyPolicy, svc.Spec.IPFamilies)

	if !EqualLoadBalancerEndpoints(loadBalancer.Spec.Endpoints, nodeEPs) {
		if err = r.patchLoadBalancerEndpoints(ctx, loadBalancer, nodeEPs); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(&svc, coreV1.EventTypeNormal, "update", "LoadBalancer endpoints successfully synced with nodes addresses")
		return ctrl.Result{Requeue: true}, nil
	}
	err = r.reconcileInfrastructure(ctx, &loadBalancer, infraDefaults)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileDebugSettings(ctx, &loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileOptions(ctx, &loadBalancer, svc)
	if err != nil {
		return ctrl.Result{}, err
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
	externalIP := getExternalIP(*svc)
	debugSettings := getDebugSettings(*svc)
	options := getOptions(*svc)
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
			Replicas: r.getReplicas(svc),
			Selector: v1.LabelSelector{
				MatchLabels: map[string]string{
					LoadBalancerLabelName: hashstring,
				},
			},
			ExternalIP:               externalIP,
			InternalLB:               *infraConfig.InternalLB,
			Endpoints:                nil,
			Ports:                    nil,
			LoadBalancerSourceRanges: nil,
			Infrastructure: yawolv1beta1.LoadBalancerInfrastructure{
				FloatingNetID: infraConfig.FloatingNetworkId,
				NetworkID:     *infraConfig.NetworkId,
				Flavor:        infraConfig.FlavorRef,
				Image:         infraConfig.ImageRef,
				AuthSecretRef: coreV1.SecretReference{
					Name:      *infraConfig.AuthSecretName,
					Namespace: *infraConfig.Namespace,
				},
			},
			DebugSettings: debugSettings,
			Options:       options,
		},
		Status: yawolv1beta1.LoadBalancerStatus{},
	}
	return r.ControlClient.Create(ctx, &loadBalancer, &client.CreateOptions{})
}

func getLoadBalancerNameFromServiceNamespacedName(namespacedName types.NamespacedName) string {
	return namespacedName.Namespace + "--" + namespacedName.Name
}

func getLoadBalancerNamespacedName(infraConfig *InfrastructureDefaults, svc *coreV1.Service) types.NamespacedName {
	return types.NamespacedName{
		Namespace: *infraConfig.Namespace,
		Name: getLoadBalancerNameFromServiceNamespacedName(
			types.NamespacedName{
				Namespace: svc.Namespace,
				Name:      svc.Name,
			},
		),
	}
}

func (r *ServiceReconciler) getReplicas(service *coreV1.Service) int {
	replicaString, found := service.Annotations[yawolv1beta1.ServiceReplicas]
	if !found {
		return 1
	}

	replicas, err := strconv.Atoi(replicaString)
	if err != nil {
		r.Log.Info("error parsing replicas from service annotation", "annotation", replicaString)
		return 1
	}

	return replicas
}

func (r *ServiceReconciler) reconcileInfrastructure(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	infraConfig InfrastructureDefaults,
) error {
	newInfra := yawolv1beta1.LoadBalancerInfrastructure{
		FloatingNetID: infraConfig.FloatingNetworkId,
		NetworkID:     *infraConfig.NetworkId,
		Flavor:        infraConfig.FlavorRef,
		Image:         infraConfig.ImageRef,
		AuthSecretRef: coreV1.SecretReference{
			Name:      *infraConfig.AuthSecretName,
			Namespace: *infraConfig.Namespace,
		},
	}
	if !reflect.DeepEqual(newInfra, lb.Spec.Infrastructure) {
		newInfraJson, err := json.Marshal(newInfra)
		if err != nil {
			return err
		}
		patch := []byte(`{"spec":{"infrastructure":` + string(newInfraJson) + `}}`)
		err = r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return err
		}
	}

	if infraConfig.InternalLB != nil && *infraConfig.InternalLB != lb.Spec.InternalLB {
		patch := []byte(`{"spec":{"internalLB":` + strconv.FormatBool(*infraConfig.InternalLB) + `}}`)
		err := r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ServiceReconciler) reconcileOptions(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc coreV1.Service,
) error {
	newOptions := getOptions(svc)
	if !reflect.DeepEqual(newOptions, lb.Spec.Options) {
		newOptionsJson, err := json.Marshal(newOptions)
		if err != nil {
			return err
		}

		if string(newOptionsJson) == "{}" {
			patch := []byte(`[{"op":"remove", "path":"/spec/options"}]`)
			return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.JSONPatchType, patch))
		}
		patch := []byte(`[{"op":"replace", "path":"/spec/options", "value": ` + string(newOptionsJson) + `}]`)
		return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.JSONPatchType, patch))
	}
	return nil
}

func (r *ServiceReconciler) reconcileDebugSettings(
	ctx context.Context,
	lb *yawolv1beta1.LoadBalancer,
	svc coreV1.Service,
) error {
	newDebugSettings := getDebugSettings(svc)
	if !reflect.DeepEqual(newDebugSettings, lb.Spec.DebugSettings) {
		newDebugSettingsJson, err := json.Marshal(newDebugSettings)
		if err != nil {
			return err
		}
		patch := []byte(`{"spec":{"debugSettings":` + string(newDebugSettingsJson) + `}}`)
		return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
	}
	return nil
}

func (r *ServiceReconciler) patchLoadBalancerPorts(ctx context.Context, lb yawolv1beta1.LoadBalancer, svcPorts []coreV1.ServicePort) error {
	svcPortsJson, err := json.Marshal(svcPorts)
	if err != nil {
		return err
	}
	patch := []byte(`{"spec":{"ports":` + string(svcPortsJson) + `}}`)

	return r.ControlClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *ServiceReconciler) patchLoadBalancerSourceRanges(ctx context.Context, lb yawolv1beta1.LoadBalancer, svcRanges []string) error {
	data, err := json.Marshal(svcRanges)
	if err != nil {
		return err
	}
	patch := []byte(`{"spec":{"loadBalancerSourceRanges":` + string(data) + `}}`)

	return r.ControlClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *ServiceReconciler) patchLoadBalancerReplicas(ctx context.Context, lb yawolv1beta1.LoadBalancer, replicas int) error {
	patch := []byte(`{"spec":{"replicas":` + fmt.Sprint(replicas) + `}}`)

	return r.ControlClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))
}

func (r *ServiceReconciler) patchLoadBalancerEndpoints(
	ctx context.Context,
	lb yawolv1beta1.LoadBalancer,
	eps []yawolv1beta1.LoadBalancerEndpoint,
) error {
	endpointsJson, err := json.Marshal(eps)
	if err != nil {
		return err
	}
	patch := []byte(`{"spec":{"endpoints":` + string(endpointsJson) + `}}`)

	return r.ControlClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))
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

func hasFinalizer(objectMeta v1.ObjectMeta, finalizer string) bool {
	for _, f := range objectMeta.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func (r ServiceReconciler) addFinalizer(ctx context.Context, svc coreV1.Service, finalizer string) error {
	patch := []byte(`{"metadata":{"finalizers": ["` + finalizer + `"]}}`)
	return r.TargetClient.Patch(ctx, &svc, client.RawPatch(types.MergePatchType, patch))
}

func (r ServiceReconciler) removeFinalizer(ctx context.Context, svc coreV1.Service, finalizer string) error {
	patch := []byte(`{"metadata":{"$deleteFromPrimitiveList/finalizers": ["` + finalizer + `"]}}`)
	return r.TargetClient.Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
}

func (r ServiceReconciler) removeIngressIPFromStatus(ctx context.Context, svc coreV1.Service) error {
	patch := []byte(`[{"op":"remove", "path":"/status/loadBalancer/ingress"}]`)
	return r.TargetClient.Status().Patch(ctx, &svc, client.RawPatch(types.JSONPatchType, patch))
}

func (r ServiceReconciler) deletionRoutine(
	ctx context.Context,
	svc coreV1.Service,
	infraDefaults InfrastructureDefaults,
) (ctrl.Result, error) {
	var loadBalancer yawolv1beta1.LoadBalancer
	var err error
	if err = r.ControlClient.Get(
		ctx,
		getLoadBalancerNamespacedName(&infraDefaults, &svc),
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
			if err = r.removeIngressIPFromStatus(ctx, svc); err != nil {
				return ctrl.Result{}, err
			}
			r.Log.Info("successfully deleted loadbalancer ip on svc status")
		}
		r.Log.Info("load balancer already deleted")
		if hasFinalizer(svc.ObjectMeta, ServiceFinalizer) {
			if err = r.removeFinalizer(ctx, svc, ServiceFinalizer); err != nil {
				return ctrl.Result{}, err
			}
			r.Log.Info("successfully deleted finalizer on svc")
			return ctrl.Result{}, nil
		}
		r.Log.Info("finalizers already removed on svc. nothing to do.")
		return ctrl.Result{}, nil
	}

	if loadBalancer.DeletionTimestamp == nil {
		if err = r.ControlClient.Delete(ctx, &loadBalancer, &client.DeleteOptions{}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func hasAnnotation(objectMeta v1.ObjectMeta, label, labelValue string) bool {
	for key, value := range objectMeta.Annotations {
		if key == label {
			return value == labelValue
		}
	}
	return false
}

func (r ServiceReconciler) addAnnotation(ctx context.Context, lb yawolv1beta1.LoadBalancer, key, value string) error {
	keyJson, err := json.Marshal(key)
	if err != nil {
		return err
	}
	valueJson, err := json.Marshal(value)
	if err != nil {
		return err
	}
	patch := []byte(`{"metadata": {"annotations": {` + string(keyJson) + `: ` + string(valueJson) + `}}}`)
	return r.ControlClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))
}

func validateService(svc coreV1.Service) error {
	for _, port := range svc.Spec.Ports {
		switch port.Protocol {
		case coreV1.ProtocolTCP:
		case coreV1.ProtocolUDP:
		default:
			return fmt.Errorf("unsupported protocol %v used (TCP and UDP is supported)", port.Protocol)
		}
	}
	return nil
}

// getExternalIP return external ip from service (Spec.LoadBalancerIP and Status.LoadBalancer.Ingress[0].IP)
// If both are set the IP from spec is used
func getExternalIP(svc coreV1.Service) *string {
	if svc.Spec.LoadBalancerIP != "" {
		return &svc.Spec.LoadBalancerIP
	}
	if len(svc.Status.LoadBalancer.Ingress) > 0 &&
		svc.Status.LoadBalancer.Ingress[0].IP != "" {
		return &svc.Status.LoadBalancer.Ingress[0].IP
	}
	return nil
}

// getDebugSettings return loadbalancer debug settings
func getDebugSettings(svc coreV1.Service) yawolv1beta1.LoadBalancerDebugSettings {
	debugSettings := yawolv1beta1.LoadBalancerDebugSettings{}
	if svc.Annotations[yawolv1beta1.ServiceDebug] == "true" ||
		svc.Annotations[yawolv1beta1.ServiceDebug] == "True" {
		debugSettings.Enabled = true
		if svc.Annotations[yawolv1beta1.ServiceDebugSSHKey] != "" {
			debugSettings.SshkeyName = svc.Annotations[yawolv1beta1.ServiceDebugSSHKey]
		}
	}
	return debugSettings
}

// getOptions return loadbalancer option settings
func getOptions(svc coreV1.Service) yawolv1beta1.LoadBalancerOptions {
	options := yawolv1beta1.LoadBalancerOptions{}
	if svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocol] != "" {
		options.TCPProxyProtocol, _ = strconv.ParseBool(svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocol])
	}
	if svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocolPortsFilter] != "" {
		options.TCPProxyProtocolPortsFilter = getTCPProxyProtocolPortsFilter(
			svc.Annotations[yawolv1beta1.ServiceTCPProxyProtocolPortsFilter],
		)
	}
	return options
}

// getTCPProxyProtocolPortsFilter return port list from annotation
func getTCPProxyProtocolPortsFilter(tcpProxyProtocolPortsFilter string) []int32 {
	if tcpProxyProtocolPortsFilter == "" {
		return nil
	}
	var portFilter []int32
	for _, port := range strings.Split(tcpProxyProtocolPortsFilter, ",") {
		intPort, err := strconv.Atoi(port)
		if err != nil {
			return nil
		}
		portFilter = append(portFilter, int32(intPort))
	}
	return portFilter
}
