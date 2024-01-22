package loadbalancerset

import (
	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	"github.com/stackitcloud/yawol/internal/helper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	newCondition.Type = helper.DeletionMarkerCondition

	existingCondition := findDeletionCondition(machine)
	if existingCondition == nil {
		if newCondition.LastTransitionTime.IsZero() {
			newCondition.LastTransitionTime = metav1.Now()
		}
		*machine.Status.Conditions = append(*machine.Status.Conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		if !newCondition.LastTransitionTime.IsZero() {
			existingCondition.LastTransitionTime = newCondition.LastTransitionTime
		} else {
			existingCondition.LastTransitionTime = metav1.Now()
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
