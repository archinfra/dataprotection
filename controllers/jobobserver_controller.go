package controllers

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

// JobObserverReconciler is the compatibility bridge for policy-created CronJob Jobs.
// It no longer finalizes backups or creates Snapshots. Instead, each scheduled native
// Job is materialized as a BackupExecution and the BackupExecution controller owns
// the execution lifecycle from that point onward.
type JobObserverReconciler struct {
	client.Client
}

func (r *JobObserverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var nativeJob batchv1.Job
	if err := r.Get(ctx, req.NamespacedName, &nativeJob); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if nativeJob.Labels[managedByLabel] != managedByValue {
		return ctrl.Result{}, nil
	}
	if nativeJob.Labels[operationLabel] != dpv1alpha1.BuildLabelValue("backup") {
		return ctrl.Result{}, nil
	}
	if nativeJob.Labels[executionKindLabel] != dpv1alpha1.BuildLabelValue("BackupPolicy") {
		return ctrl.Result{}, nil
	}

	policyName := trimString(nativeJob.Labels[policyNameLabel])
	sourceName := trimString(nativeJob.Labels[sourceNameLabel])
	storageName := trimString(nativeJob.Labels[storageNameLabel])
	if policyName == "" || sourceName == "" || storageName == "" {
		return ctrl.Result{}, nil
	}

	policy, err := getBackupPolicy(ctx, r.Client, nativeJob.Namespace, policyName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	notificationNames := parseNotificationAnnotation(nativeJob.Annotations[notificationRefsAnnotation])
	notificationRefs := make([]corev1.LocalObjectReference, 0, len(notificationNames))
	for _, name := range notificationNames {
		notificationRefs = append(notificationRefs, corev1.LocalObjectReference{Name: name})
	}
	if len(notificationRefs) == 0 {
		notificationRefs = append(notificationRefs, policy.Spec.NotificationRefs...)
	}

	execution := &dpv1alpha1.BackupExecution{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nativeJob.Name,
			Namespace: nativeJob.Namespace,
			Labels: map[string]string{
				managedByLabel:   managedByValue,
				operationLabel:   dpv1alpha1.BuildLabelValue("backup"),
				sourceNameLabel:  dpv1alpha1.BuildLabelValue(sourceName),
				storageNameLabel: dpv1alpha1.BuildLabelValue(storageName),
				policyNameLabel:  dpv1alpha1.BuildLabelValue(policyName),
			},
			Annotations: map[string]string{
				adoptedNativeJobAnnotation: nativeJob.Name,
			},
		},
		Spec: dpv1alpha1.BackupExecutionSpec{
			PolicyRef:        &corev1.LocalObjectReference{Name: policy.Name},
			SourceRef:        corev1.LocalObjectReference{Name: sourceName},
			StorageRef:       corev1.LocalObjectReference{Name: storageName},
			RetentionRef:     policy.Spec.RetentionRef,
			NotificationRefs: notificationRefs,
			JobRuntime:       policy.Spec.JobRuntime,
			Reason:           fmt.Sprintf("scheduled by BackupPolicy/%s", policy.Name),
			Trigger:          dpv1alpha1.BackupExecutionTriggerScheduled,
		},
	}

	var current dpv1alpha1.BackupExecution
	key := client.ObjectKeyFromObject(execution)
	if err := r.Get(ctx, key, &current); err == nil {
		return ctrl.Result{}, nil
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, execution); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func parseNotificationAnnotation(value string) []string {
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := trimString(part); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (r *JobObserverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.Job{}).
		Complete(r)
}
