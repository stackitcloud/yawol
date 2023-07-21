package helper

import (
	"context"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewLoadBalancerSetForLoadBalancer(lb *yawolv1beta1.LoadBalancer, hash string, revision int) *yawolv1beta1.LoadBalancerSet {
	setLabels := GetLoadBalancerSetLabelsFromLoadBalancer(lb)
	setLabels[HashLabel] = hash

	return &yawolv1beta1.LoadBalancerSet{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      lb.Name + "-" + hash,
			Namespace: lb.Namespace,
			OwnerReferences: []metaV1.OwnerReference{
				GetOwnersReferenceForLB(lb),
			},
			Labels: setLabels,
			Annotations: map[string]string{
				RevisionAnnotation: strconv.Itoa(revision),
			},
		},
		Spec: yawolv1beta1.LoadBalancerSetSpec{
			Selector: metaV1.LabelSelector{
				MatchLabels: setLabels,
			},
			Template: yawolv1beta1.LoadBalancerMachineTemplateSpec{
				Labels: setLabels,
				Spec: yawolv1beta1.LoadBalancerMachineSpec{
					Infrastructure: lb.Spec.Infrastructure,
					PortID:         *lb.Status.PortID,
					ServerGroupID:  pointer.StringDeref(lb.Status.ServerGroupID, ""),
					LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
						Namespace: lb.Namespace,
						Name:      lb.Name,
					},
				},
			},
			Replicas: lb.Spec.Replicas,
		},
	}
}

// PatchLoadBalancerSetReplicas sets replicas in LoadBalancerSet
func PatchLoadBalancerSetReplicas(ctx context.Context, c client.Client, lbs *yawolv1beta1.LoadBalancerSet, replicas int) error {
	patch := []byte(`{"spec": {"replicas": ` + strconv.Itoa(replicas) + `}}`)
	return c.Patch(ctx, lbs, client.RawPatch(types.MergePatchType, patch))
}

// ScaleDownOldLoadBalancerSets scales down all LoadBalancerSets in the given list except the one with the given name.
func ScaleDownOldLoadBalancerSets(
	ctx context.Context,
	c client.Client,
	setList *yawolv1beta1.LoadBalancerSetList,
	currentSetName string,
) error {
	for i := range setList.Items {
		if setList.Items[i].Name != currentSetName && setList.Items[i].Spec.Replicas > 0 {
			if err := PatchLoadBalancerSetReplicas(ctx, c, &setList.Items[i], 0); err != nil {
				return err
			}
		}
	}

	return nil
}

// LBSetHasKeepalivedMaster returns true one of the following conditions are met:
// - if the keepalived condition on set is ready for more than 2 min
// - keepalived condition is not ready for more than 10 min (to make sure this does not block updates)
// - no keepalived condition is in lbs but lbs is older than 15 min (to make sure this does not block updates)
func LBSetHasKeepalivedMaster(set *yawolv1beta1.LoadBalancerSet) bool {
	before2Minutes := metaV1.Time{Time: time.Now().Add(-2 * time.Minute)}
	before10Minutes := metaV1.Time{Time: time.Now().Add(-10 * time.Minute)}
	before15Minutes := metaV1.Time{Time: time.Now().Add(-15 * time.Minute)}

	for _, condition := range set.Status.Conditions {
		if condition.Type != HasKeepalivedMaster {
			continue
		}
		if condition.Status == metaV1.ConditionTrue {
			return condition.LastTransitionTime.Before(&before2Minutes)
		}
		return condition.LastTransitionTime.Before(&before10Minutes)
	}
	return set.CreationTimestamp.Before(&before15Minutes)
}

// StatusReplicasFromSetList returns the total replicas and ready replicas based on the given list of LoadBalancerSets.
func StatusReplicasFromSetList(setList *yawolv1beta1.LoadBalancerSetList) (replicas, readyReplicas int) {
	for i := range setList.Items {
		replicas += pointer.IntDeref(setList.Items[i].Status.Replicas, 0)
		readyReplicas += pointer.IntDeref(setList.Items[i].Status.ReadyReplicas, 0)
	}

	return
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
func GetLoadBalancerSetForHash(
	loadBalancerSetList *yawolv1beta1.LoadBalancerSetList,
	currentHash string,
) (*yawolv1beta1.LoadBalancerSet, error) {
	var (
		highestGen    int
		highestGenLBS *yawolv1beta1.LoadBalancerSet
	)

	for i := range loadBalancerSetList.Items {
		if loadBalancerSetList.Items[i].Labels[HashLabel] != currentHash {
			continue
		}

		if gen, err := strconv.Atoi(loadBalancerSetList.Items[i].Annotations[RevisionAnnotation]); err == nil && highestGen < gen {
			highestGen = gen
			highestGenLBS = loadBalancerSetList.Items[i].DeepCopy()
		}
	}

	return highestGenLBS, nil
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
	metrics *helpermetrics.LoadBalancerSetMetricList,
) {
	if metrics == nil ||
		metrics.ReplicasMetrics == nil ||
		metrics.ReplicasCurrentMetrics == nil ||
		metrics.ReplicasReadyMetrics == nil ||
		metrics.DeletionTimestampMetrics == nil {
		return
	}

	specReplicas := lbs.Spec.Replicas
	if lbs.DeletionTimestamp != nil {
		// If deletionTimestamp is set, the desired state is to have 0 replicas. Setting this metrics to 0 makes it easier
		// to define alerts that should not trigger false positives, if a LoadBalancer takes a long time to delete.
		specReplicas = 0
	}
	metrics.ReplicasMetrics.
		WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
		Set(float64(specReplicas))

	if lbs.Status.Replicas != nil {
		metrics.ReplicasCurrentMetrics.
			WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
			Set(float64(*lbs.Status.Replicas))
	}
	if lbs.Status.ReadyReplicas != nil {
		metrics.ReplicasReadyMetrics.
			WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
			Set(float64(*lbs.Status.ReadyReplicas))
	}

	if lbs.DeletionTimestamp != nil {
		metrics.DeletionTimestampMetrics.
			WithLabelValues(lbs.Spec.Template.Spec.LoadBalancerRef.Name, lbs.Name, lbs.Namespace).
			Set(float64(lbs.DeletionTimestamp.Unix()))
	}
}

func RemoveLoadBalancerSetMetrics(
	lbs yawolv1beta1.LoadBalancerSet,
	metrics *helpermetrics.LoadBalancerSetMetricList,
) {
	if metrics == nil ||
		metrics.ReplicasMetrics == nil ||
		metrics.ReplicasCurrentMetrics == nil ||
		metrics.ReplicasReadyMetrics == nil ||
		metrics.DeletionTimestampMetrics == nil {
		return
	}

	l := map[string]string{
		"lb":        lbs.Spec.Template.Spec.LoadBalancerRef.Name,
		"lbs":       lbs.Name,
		"namespace": lbs.Namespace,
	}
	metrics.ReplicasMetrics.DeletePartialMatch(l)
	metrics.ReplicasCurrentMetrics.DeletePartialMatch(l)
	metrics.ReplicasReadyMetrics.DeletePartialMatch(l)
	metrics.DeletionTimestampMetrics.DeletePartialMatch(l)
}
