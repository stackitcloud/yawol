package targetcontroller

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"

	"github.com/go-logr/logr"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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

//nolint:lll // because regex, thats why
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

	if len(loadBalancers.Items) == 0 {
		// skip of no LBs are present
		return ctrl.Result{}, nil
	}

	var nodes coreV1.NodeList
	if err := r.TargetClient.List(ctx, &nodes, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, err
	}

	for i := range loadBalancers.Items {
		// get svc
		svc := coreV1.Service{}
		serviceAnnotation := strings.Split(loadBalancers.Items[i].Annotations[ServiceAnnotation], "/")
		if len(serviceAnnotation) != 2 {
			return ctrl.Result{}, helper.ErrCouldNotReadSvcNameSpacedNameFromAnno
		}
		if err := r.TargetClient.Get(ctx, client.ObjectKey{Name: serviceAnnotation[1], Namespace: serviceAnnotation[0]}, &svc); err != nil {
			return ctrl.Result{}, err
		}

		readyEndpoints := getReadyEndpointsFromNodes(nodes.Items, svc.Spec.IPFamilies)

		// update endpoints
		if !EqualLoadBalancerEndpoints(loadBalancers.Items[i].Spec.Endpoints, readyEndpoints) {
			if err := r.patchEndpointsToLB(ctx, readyEndpoints, &loadBalancers.Items[i]); err != nil {
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
		WithEventFilter(yawolNodePredicate()).
		Complete(r)
}

func yawolNodePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			newNode := updateEvent.ObjectNew.(*coreV1.Node)
			oldNode := updateEvent.ObjectOld.(*coreV1.Node)

			if isNodeReady(*oldNode) != isNodeReady(*newNode) {
				return true
			}

			return !reflect.DeepEqual(
				getLoadBalancerEndpointFromNode(*oldNode, []coreV1.IPFamily{}),
				getLoadBalancerEndpointFromNode(*newNode, []coreV1.IPFamily{}),
			)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}
}

func (r *NodeReconciler) patchEndpointsToLB(
	ctx context.Context,
	endpoints []yawolv1beta1.LoadBalancerEndpoint,
	lb *yawolv1beta1.LoadBalancer,
) error {
	var epJSON []byte
	var err error
	if epJSON, err = json.Marshal(endpoints); err != nil {
		return err
	}

	patch := []byte(`{"spec":{"endpoints":` + string(epJSON) + `}}`)

	return r.ControlClient.Patch(ctx, lb, client.RawPatch(types.MergePatchType, patch))
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
	ipFamilies []coreV1.IPFamily,
) []yawolv1beta1.LoadBalancerEndpoint {
	// TODO check if ipFamilies and IPFamilyPolicyType is available?
	eps := make([]yawolv1beta1.LoadBalancerEndpoint, 0)
	for i := range nodes {
		if !isNodeReady(nodes[i]) {
			continue
		}
		eps = append(eps, getLoadBalancerEndpointFromNode(nodes[i], ipFamilies))
	}

	return eps
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
