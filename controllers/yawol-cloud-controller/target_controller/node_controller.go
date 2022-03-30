package target_controller

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeReconciler reconciles service Objects with type LoadBalancer
type NodeReconciler struct {
	TargetClient           client.Client
	ControlClient          client.Client
	InfrastructureDefaults InfrastructureDefaults
	Log                    logr.Logger
	Scheme                 *runtime.Scheme
	Recorder               record.EventRecorder
}

// nolint: lll // because regex, thats why
var ipv6Regex = `^(([0-9a-fA-F]{1,4}:){7,7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(:[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(ffff(:0{1,4}){0,1}:){0,1}((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])|([0-9a-fA-F]{1,4}:){1,4}:((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9]))$`
var ipv4Regex = `^(((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.|$)){4})`
var ipv6RegexC, _ = regexp.Compile(ipv6Regex)
var ipv4RegexC, _ = regexp.Compile(ipv4Regex)

// +kubebuilder:rbac:groups=core,resources=node,verbs=get;list;watch
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("node", req.NamespacedName)

	var loadBalancers yawolv1beta1.LoadBalancerList
	if err := r.ControlClient.List(ctx, &loadBalancers, &client.ListOptions{
		Namespace: *r.InfrastructureDefaults.Namespace,
	}); err != nil {
		return ctrl.Result{}, err
	}

	var nodes coreV1.NodeList
	if err := r.TargetClient.List(ctx, &nodes, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, err
	}

	for _, loadBalancer := range loadBalancers.Items {
		// get svc
		svc := coreV1.Service{}
		serviceAnnotation := strings.Split(loadBalancer.Annotations[ServiceAnnotation], "/")
		if len(serviceAnnotation) != 2 {
			return ctrl.Result{}, errors.New("could not read service namespacedname from annotation")
		}
		if err := r.TargetClient.Get(ctx, client.ObjectKey{Name: serviceAnnotation[1], Namespace: serviceAnnotation[0]}, &svc); err != nil {
			return ctrl.Result{}, err
		}

		readyEndpoints := getReadyEndpointsFromNodes(nodes.Items, svc.Spec.IPFamilyPolicy, svc.Spec.IPFamilies)

		// update endpoints
		if !EqualLoadBalancerEndpoints(loadBalancer.Spec.Endpoints, readyEndpoints) {
			if err := r.addEndpointsToLB(ctx, readyEndpoints, loadBalancer); err != nil {
				return ctrl.Result{}, err
			}

			r.Recorder.Event(&svc, coreV1.EventTypeNormal, "update", "LoadBalancer endpoints successfully synced with nodes addresses")
		}
	}

	return ctrl.Result{}, nil
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&coreV1.Node{}).
		Complete(r)
}

func (r *NodeReconciler) addEndpointsToLB(
	ctx context.Context,
	endpoints []yawolv1beta1.LoadBalancerEndpoint,
	lb yawolv1beta1.LoadBalancer,
) error {
	var epJson []byte
	var err error
	if epJson, err = json.Marshal(endpoints); err != nil {
		return err
	}

	patch := []byte(`{"spec":{"endpoints":` + string(epJson) + `}}`)

	return r.ControlClient.Patch(ctx, &lb, client.RawPatch(types.MergePatchType, patch))
}

func isNodeReady(node coreV1.Node) bool {
	if node.DeletionTimestamp != nil {
		return false
	}

	for _, condition := range node.Status.Conditions {
		if condition.Type == coreV1.NodeReady {
			return condition.Status == coreV1.ConditionTrue
		}
	}
	return false
}

func getLoadBalancerEndpointFromNode(node coreV1.Node, ipFamilies []coreV1.IPFamily) yawolv1beta1.LoadBalancerEndpoint {
	lbEndpoint := yawolv1beta1.LoadBalancerEndpoint{
		Name:      node.Name,
		Addresses: []string{},
	}

	for _, address := range node.Status.Addresses {
		if address.Type != coreV1.NodeInternalIP {
			continue
		}

		// this should never happen since k8s autofills this field if it is nil
		if len(ipFamilies) == 0 {
			lbEndpoint.Addresses = append(lbEndpoint.Addresses, address.Address)
			continue
		}

		for _, ipFamily := range ipFamilies {
			if ipFamily == coreV1.IPv4Protocol {
				if ipv4RegexC.MatchString(address.Address) {
					lbEndpoint.Addresses = append(lbEndpoint.Addresses, address.Address)
				}
				continue
			}

			if ipFamily == coreV1.IPv6Protocol {
				if ipv6RegexC.MatchString(address.Address) {
					lbEndpoint.Addresses = append(lbEndpoint.Addresses, address.Address)
				}
				continue
			}
		}
	}

	return lbEndpoint
}

func getReadyEndpointsFromNodes(
	nodes []coreV1.Node,
	// nolint: unparam // will be used in the future
	ipFamilyType *coreV1.IPFamilyPolicyType,
	ipFamilies []coreV1.IPFamily,
) []yawolv1beta1.LoadBalancerEndpoint {
	// TODO check if ipFamilies and IPFamilyPolicyType is available?
	eps := make([]yawolv1beta1.LoadBalancerEndpoint, 0)
	for _, node := range nodes {
		if !isNodeReady(node) {
			continue
		}

		eps = append(eps, getLoadBalancerEndpointFromNode(node, ipFamilies))
	}

	return eps
}
