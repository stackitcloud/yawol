package loadbalancerset

import (
	"fmt"
	"time"

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

// areRelevantConditionsMet checks if all required conditions (from the
// perspective of the LoadBalancerSet) are both `True` and up-to-date, according
// to the passed expiration time. If `stableConditions` is set, a condition is
// only considered `False` if it has been in that state since the expiration
// time.
func areRelevantConditionsMet(machine *yawolv1beta1.LoadBalancerMachine, expiration metav1.Time, stableConditions bool) (bool, error) {
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

		conditionIsStable := true
		if stableConditions {
			conditionIsStable = condition.LastTransitionTime.Before(&expiration)
		}
		if conditionIsStable && condition.Status != corev1.ConditionTrue {
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

func findDeletionCondition(machine *yawolv1beta1.LoadBalancerMachine) *corev1.NodeCondition {
	if machine.Status.Conditions == nil {
		return nil
	}
	conditions := *machine.Status.Conditions
	for i := range conditions {
		if conditions[i].Type == helper.DeletionMarkerCondition {
			return &conditions[i]
		}
	}
	return nil
}

func setDeletionCondition(machine *yawolv1beta1.LoadBalancerMachine, newCondition corev1.NodeCondition) {
	if machine.Status.Conditions == nil {
		machine.Status.Conditions = &[]corev1.NodeCondition{}
	}

	newCondition.LastHeartbeatTime = metav1.Now()
	newCondition.Type = helper.DeletionMarkerCondition

	existingCondition := findDeletionCondition(machine)
	if existingCondition == nil {
		if newCondition.LastTransitionTime.IsZero() {
			newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		*machine.Status.Conditions = append(*machine.Status.Conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		if !newCondition.LastTransitionTime.IsZero() {
			existingCondition.LastTransitionTime = newCondition.LastTransitionTime
		} else {
			existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
	}

	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
	existingCondition.LastHeartbeatTime = newCondition.LastHeartbeatTime
}

func removeDeletionCondition(machine *yawolv1beta1.LoadBalancerMachine) {
	if machine.Status.Conditions == nil || len(*machine.Status.Conditions) == 0 {
		return
	}
	newConditions := make([]corev1.NodeCondition, 0, len(*machine.Status.Conditions)-1)
	for _, condition := range *machine.Status.Conditions {
		if condition.Type != helper.DeletionMarkerCondition {
			newConditions = append(newConditions, condition)
		}
	}

	*machine.Status.Conditions = newConditions
}
