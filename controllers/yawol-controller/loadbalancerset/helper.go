package loadbalancerset

import (
	"fmt"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var relevantLBMConditionslForLBS = []helper.LoadbalancerCondition{
	helper.ConfigReady,
	helper.EnvoyReady,
	helper.EnvoyUpToDate,
}

// areRelevantConditionsMet returns if all required conditions (from the
// perspective of the LoadBalancerSet) are both `True` and up-to-date, according
// to the passed expiration time.
func areRelevantConditionsMet(machine *yawolv1beta1.LoadBalancerMachine, expiration metav1.Time, checkTransition bool) (bool, error) {
	if machine.Status.Conditions == nil {
		return false, fmt.Errorf("no conditions set")
	}

	// constuct lookup map
	conditions := *machine.Status.Conditions
	condMap := make(map[helper.LoadbalancerCondition]corev1.NodeCondition, len(conditions))
	for i := range conditions {
		condMap[helper.LoadbalancerCondition(conditions[i].Type)] = conditions[i]
	}

	for _, typ := range relevantLBMConditionslForLBS {
		condition, found := condMap[typ]
		if !found {
			return false, fmt.Errorf("required condition %s not present on machine", typ)
		}

		transitionCheck := true
		if checkTransition {
			transitionCheck = condition.LastTransitionTime.Before(&expiration)
		}
		if transitionCheck && condition.Status != corev1.ConditionTrue {
			return false, fmt.Errorf(
				"condition: %v, reason: %v, status: %v, message: %v, lastTransitionTime: %v - %w",
				condition.Type, condition.Reason, condition.Status, condition.Message, condition.LastTransitionTime,
				helper.ErrConditionsNotInCorrectState,
			)
		}
		if condition.LastHeartbeatTime.Before(&expiration) {
			return false, fmt.Errorf(
				"no condition heartbeat in the last 5 min: %w",
				helper.ErrConditionsLastHeartbeatTimeToOld,
			)
		}
	}

	return true, nil
}

func getDeletionCondition(machine *yawolv1beta1.LoadBalancerMachine) (bool, *corev1.NodeCondition) {
	if machine.Status.Conditions == nil {
		return false, nil
	}
	conditions := *machine.Status.Conditions
	for i := range conditions {
		if conditions[i].Type == deletionMarkerCondition {
			return true, &conditions[i]
		}
	}
	return false, nil
}

func setDeletionCondition(machine *yawolv1beta1.LoadBalancerMachine, status corev1.ConditionStatus, reason, message string) {
	if machine.Status.Conditions == nil {
		machine.Status.Conditions = &[]corev1.NodeCondition{}
	}
	var cond *corev1.NodeCondition
	conditions := *machine.Status.Conditions
	if ok, foundCondition := getDeletionCondition(machine); ok {
		cond = foundCondition
	} else {
		// TODO: do we need to sort the slice again?
		conditions = append(conditions, corev1.NodeCondition{
			Type: deletionMarkerCondition,
		})
		cond = &(conditions)[len(conditions)-1]
	}

	if cond.Status != status {
		cond.LastTransitionTime = metav1.Now()
	}
	cond.LastHeartbeatTime = metav1.Now()
	cond.Status = status
	cond.Reason = reason
	cond.Message = message
	machine.Status.Conditions = &conditions
}
