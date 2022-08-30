package helper

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateLoadBalancerSet(
	ctx context.Context,
	c client.Client,
	lb *yawolv1beta1.LoadBalancer,
	machineSpec *yawolv1beta1.LoadBalancerMachineSpec,
	hash string,
	revision int,
) error {
	lbsetLabels := GetLoadBalancerSetLabelsFromLoadBalancer(lb)
	lbsetLabels[HashLabel] = hash

	lbset := yawolv1beta1.LoadBalancerSet{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      lb.Name + "-" + hash,
			Namespace: lb.Namespace,
			OwnerReferences: []metaV1.OwnerReference{
				GetOwnersReferenceForLB(lb),
			},
			Labels: lbsetLabels,
			Annotations: map[string]string{
				RevisionAnnotation: strconv.Itoa(revision),
			},
		},
		Spec: yawolv1beta1.LoadBalancerSetSpec{
			Selector: metaV1.LabelSelector{
				MatchLabels: lbsetLabels,
			},
			Template: yawolv1beta1.LoadBalancerMachineTemplateSpec{
				Labels: lbsetLabels,
				Spec:   *machineSpec,
			},
			Replicas: lb.Spec.Replicas,
		},
	}
	if err := c.Create(ctx, &lbset); err != nil {
		return err
	}
	return nil
}

// PatchLoadBalancerSetReplicas sets replicas in LoadBalancerSet
func PatchLoadBalancerSetReplicas(ctx context.Context, c client.Client, lbs *yawolv1beta1.LoadBalancerSet, replicas int) error {
	patch := []byte(`{"spec": {"replicas": ` + strconv.Itoa(replicas) + `}}`)
	return c.Patch(ctx, lbs, client.RawPatch(types.MergePatchType, patch))
}

// ScaleDownAllLoadBalancerSetsForLBBut Scales down all LoadBalancerSets deriving from a LB but the one with the name of exceptionName
// This will use getLoadBalancerSetsForLoadBalancer to identify the LBS deriving from the given LB
// See error handling getLoadBalancerSetsForLoadBalancer
// See error handling patchLoadBalancerSetReplicas
// Requests requeue when a LBS has been downscaled
func ScaleDownAllLoadBalancerSetsForLBBut(
	ctx context.Context,
	c client.Client,
	lb *yawolv1beta1.LoadBalancer,
	exceptionName string,
) (ctrl.Result, error) {
	loadBalancerSetList, err := GetLoadBalancerSetsForLoadBalancer(ctx, c, lb)
	if err != nil {
		return ctrl.Result{}, err
	}

	var scaledDown bool
	for i := range loadBalancerSetList.Items {
		if loadBalancerSetList.Items[i].Name != exceptionName && loadBalancerSetList.Items[i].Spec.Replicas > 0 {
			if err := PatchLoadBalancerSetReplicas(ctx, c, &loadBalancerSetList.Items[i], 0); err != nil {
				return ctrl.Result{}, err
			}
			scaledDown = true
		}
	}

	if scaledDown {
		return ctrl.Result{RequeueAfter: DefaultRequeueTime}, nil
	}
	return ctrl.Result{}, nil
}

func LoadBalancerSetIsReady(
	ctx context.Context,
	c client.Client,
	lb *yawolv1beta1.LoadBalancer,
	currentSet *yawolv1beta1.LoadBalancerSet,
) (bool, error) {
	loadBalancerSetList, err := GetLoadBalancerSetsForLoadBalancer(ctx, c, lb)
	if err != nil {
		return false, err
	}

	for i := range loadBalancerSetList.Items {
		if loadBalancerSetList.Items[i].Name != currentSet.Name {
			continue
		}

		desiredReplicas := currentSet.Spec.Replicas

		if loadBalancerSetList.Items[i].Spec.Replicas != desiredReplicas {
			return false, nil
		}

		if loadBalancerSetList.Items[i].Status.Replicas == nil ||
			loadBalancerSetList.Items[i].Status.ReadyReplicas == nil {
			return false, nil
		}

		if *loadBalancerSetList.Items[i].Status.Replicas != desiredReplicas ||
			*loadBalancerSetList.Items[i].Status.ReadyReplicas != desiredReplicas {
			return false, nil
		}

		return true, nil
	}

	return false, fmt.Errorf("active LoadBalancerSet not found")
}

// Checks if LoadBalancerSets deriving from LoadBalancers are downscaled except for the LoadBalancerSet with the name of exceptionName
// Returns true if all are downscaled; false if not
// Follows error contract of getLoadBalancerSetsForLoadBalancer
func AreAllLoadBalancerSetsForLBButDownscaled(
	ctx context.Context,
	c client.Client,
	lb *yawolv1beta1.LoadBalancer,
	exceptionName string,
) (bool, error) {
	loadBalancerSetList, err := GetLoadBalancerSetsForLoadBalancer(ctx, c, lb)
	if err != nil {
		return false, err
	}

	for i := range loadBalancerSetList.Items {
		if loadBalancerSetList.Items[i].Name == exceptionName {
			continue
		}
		if loadBalancerSetList.Items[i].Spec.Replicas != 0 {
			return false, nil
		}
		if loadBalancerSetList.Items[i].Status.Replicas != nil && *loadBalancerSetList.Items[i].Status.Replicas != 0 {
			return false, nil
		}
		if loadBalancerSetList.Items[i].Status.ReadyReplicas != nil && *loadBalancerSetList.Items[i].Status.ReadyReplicas != 0 {
			return false, nil
		}
	}

	return true, nil
}

// This returns all LoadBalancerSets for a given LoadBalancer
// Returns an error if lb is nil
// Returns an error if lb.UID is empty
// Returns an error if kube api-server problems occurred
func GetLoadBalancerSetsForLoadBalancer(
	ctx context.Context,
	c client.Client,
	lb *yawolv1beta1.LoadBalancer,
) (yawolv1beta1.LoadBalancerSetList, error) {
	if lb == nil {
		return yawolv1beta1.LoadBalancerSetList{}, ErrLBMustNotBeNil
	}

	if lb.UID == "" {
		return yawolv1beta1.LoadBalancerSetList{}, ErrLBUIDMustNotBeNil
	}

	filerLabels := GetLoadBalancerSetLabelsFromLoadBalancer(lb)
	var loadBalancerSetList yawolv1beta1.LoadBalancerSetList
	if err := c.List(ctx, &loadBalancerSetList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(filerLabels),
		Namespace:     lb.Namespace,
	}); err != nil {
		return yawolv1beta1.LoadBalancerSetList{}, err
	}

	return loadBalancerSetList, nil
}

func GetLoadBalancerSetLabelsFromLoadBalancer(lb *yawolv1beta1.LoadBalancer) map[string]string {
	return copyLabelMap(lb.Spec.Selector.MatchLabels)
}

// Returns nil if no matching exists
// If there are multiple: Returns one with highest RevisionAnnotation annotation
// If there is a single one: returns the one fetched
func GetLoadBalancerSetForHash(
	ctx context.Context,
	c client.Client,
	filterLabels map[string]string,
	hash string,
) (*yawolv1beta1.LoadBalancerSet, error) {
	filterLabels = copyLabelMap(filterLabels)
	filterLabels[HashLabel] = hash
	var loadBalancerSetList yawolv1beta1.LoadBalancerSetList

	if err := c.List(ctx, &loadBalancerSetList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(filterLabels),
	}); err != nil {
		return nil, err
	}

	if len(loadBalancerSetList.Items) == 0 {
		return nil, nil
	}

	if len(loadBalancerSetList.Items) == 1 {
		return &loadBalancerSetList.Items[0], nil
	}

	var highestGen int
	var highestGenLBS yawolv1beta1.LoadBalancerSet
	if len(loadBalancerSetList.Items) > 1 {
		for i := range loadBalancerSetList.Items {
			if gen, err := strconv.Atoi(loadBalancerSetList.Items[i].Annotations[RevisionAnnotation]); err == nil && highestGen < gen {
				highestGen = gen
				highestGenLBS = loadBalancerSetList.Items[i]
			}
		}
	}

	return &highestGenLBS, nil
}

func copyLabelMap(lbs map[string]string) map[string]string {
	targetMap := make(map[string]string)
	for key, value := range lbs {
		targetMap[key] = value
	}
	return targetMap
}

func LoadBalancerSetConditionIsFalse(condition v1.NodeCondition) bool {
	switch string(condition.Type) {
	case string(ConfigReady):
		if string(condition.Status) != string(ConditionTrue) {
			return true
		}
	case string(EnvoyReady):
		if string(condition.Status) != string(ConditionTrue) {
			return true
		}
	case string(EnvoyUpToDate):
		if string(condition.Status) != string(ConditionTrue) {
			return true
		}
	}
	return false
}

func ParseLoadBalancerSetMetrics(
	lbs yawolv1beta1.LoadBalancerSet,
	loadBalancerSetReplicasMetrics *prometheus.GaugeVec,
	loadBalancerSetReplicasCurrentMetrics *prometheus.GaugeVec,
	loadBalancerSetReplicasReadyMetrics *prometheus.GaugeVec,
) {
	if loadBalancerSetReplicasMetrics == nil ||
		loadBalancerSetReplicasCurrentMetrics == nil ||
		loadBalancerSetReplicasReadyMetrics == nil {
		return
	}

	loadBalancerSetReplicasMetrics.
		WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
		Set(float64(lbs.Spec.Replicas))
	if lbs.Status.Replicas != nil {
		loadBalancerSetReplicasCurrentMetrics.
			WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
			Set(float64(*lbs.Status.Replicas))
	}
	if lbs.Status.ReadyReplicas != nil {
		loadBalancerSetReplicasReadyMetrics.
			WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
			Set(float64(*lbs.Status.ReadyReplicas))
	}
}

func RemoveLoadBalancerSetMetrics(
	lbs yawolv1beta1.LoadBalancerSet,
	loadBalancerSetReplicasMetrics *prometheus.GaugeVec,
	loadBalancerSetReplicasCurrentMetrics *prometheus.GaugeVec,
	loadBalancerSetReplicasReadyMetrics *prometheus.GaugeVec,
) {
	if loadBalancerSetReplicasMetrics == nil ||
		loadBalancerSetReplicasCurrentMetrics == nil ||
		loadBalancerSetReplicasReadyMetrics == nil {
		return
	}
	l := map[string]string{
		"lb":        lbs.Spec.Template.Spec.LoadBalancerRef.Name,
		"lbs":       lbs.Name,
		"namespace": lbs.Namespace,
	}
	loadBalancerSetReplicasMetrics.DeletePartialMatch(l)
	loadBalancerSetReplicasCurrentMetrics.DeletePartialMatch(l)
	loadBalancerSetReplicasReadyMetrics.DeletePartialMatch(l)
}
