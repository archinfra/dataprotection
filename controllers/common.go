package controllers

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func resolveKeepLastRetention(ctx context.Context, c client.Client, namespace string, inline dpv1alpha1.RetentionRule, refName string) (int32, *dpv1alpha1.RetentionPolicy, error) {
	keepLast := retentionValue(inline)
	refName = trimString(refName)
	if refName == "" {
		return keepLast, nil, nil
	}

	retentionPolicy, err := getRetentionPolicy(ctx, c, namespace, refName)
	if err != nil {
		return 0, nil, err
	}
	if err := retentionPolicy.Spec.ValidateBasic(); err != nil {
		return 0, retentionPolicy, newPermanentDependencyError("referenced RetentionPolicy %q is invalid: %v", retentionPolicy.Name, err)
	}
	if retentionPolicy.Spec.SuccessfulSnapshots.Last > 0 {
		keepLast = retentionPolicy.Spec.SuccessfulSnapshots.Last
	}
	return keepLast, retentionPolicy, nil
}

func trimString(value string) string {
	return strings.TrimSpace(value)
}

func localRefName(ref *corev1.LocalObjectReference) string {
	if ref == nil {
		return ""
	}
	return trimString(ref.Name)
}
