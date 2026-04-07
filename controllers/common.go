package controllers

import (
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func nowTime() *metav1.Time {
	t := metav1.NewTime(time.Now().UTC())
	return &t
}

func markCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	})
}

func phaseMessage(phase dpv1alpha1.ResourcePhase) string {
	switch phase {
	case dpv1alpha1.ResourcePhaseReady:
		return "resource is ready"
	case dpv1alpha1.ResourcePhaseRunning:
		return "resource is actively running"
	case dpv1alpha1.ResourcePhaseSucceeded:
		return "request completed successfully"
	case dpv1alpha1.ResourcePhaseFailed:
		return "resource reconciliation failed"
	case dpv1alpha1.ResourcePhasePaused:
		return "resource is suspended"
	default:
		return "resource is pending reconciliation"
	}
}

func requeueSoon() ctrl.Result {
	return ctrl.Result{RequeueAfter: 30 * time.Second}
}
