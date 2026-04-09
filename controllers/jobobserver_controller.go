package controllers

import (
	"context"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

const notificationObservedAnnotation = "dataprotection.archinfra.io/notification-phase"

type JobObserverReconciler struct {
	client.Client
	APIReader client.Reader
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

	phase, _, done := jobTerminalState(&nativeJob)
	if !done {
		return requeueSoon(), nil
	}

	sourceName := nativeJob.Labels[sourceNameLabel]
	storageName := nativeJob.Labels[storageNameLabel]
	if sourceName == "" || storageName == "" {
		return ctrl.Result{}, nil
	}
	source, err := getBackupSource(ctx, r.Client, nativeJob.Namespace, sourceName)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	storage, err := getBackupStorage(ctx, r.Client, nativeJob.Namespace, storageName)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	var retention *dpv1alpha1.RetentionPolicy
	if policyName := nativeJob.Labels[policyNameLabel]; policyName != "" {
		if policy, err := getBackupPolicy(ctx, r.Client, nativeJob.Namespace, policyName); err == nil {
			retention, _ = resolveRetentionPolicy(ctx, r.Client, nativeJob.Namespace, localRefName(policy.Spec.RetentionRef))
		}
	}
	series := nativeJob.Annotations[seriesAnnotation]
	if series == "" {
		series = buildSeries(source, storage.Name, nativeJob.Labels[policyNameLabel], nativeJob.Name)
	}
	observation, err := observeTerminalBackupJob(
		ctx,
		r.Client,
		r.APIReader,
		&nativeJob,
		source,
		storage,
		func() *corev1.LocalObjectReference {
			if policyName := nativeJob.Labels[policyNameLabel]; policyName != "" {
				return &corev1.LocalObjectReference{Name: policyName}
			}
			return nil
		}(),
		nil,
		series,
		effectiveSuccessfulKeepLast(retention),
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	if observation == nil {
		return requeueSoon(), nil
	}

	if names := parseNotificationAnnotation(nativeJob.Annotations[notificationRefsAnnotation]); len(names) > 0 && nativeJob.Annotations[notificationObservedAnnotation] != string(phase) {
		refs := make([]corev1.LocalObjectReference, 0, len(names))
		for _, name := range names {
			refs = append(refs, corev1.LocalObjectReference{Name: name})
		}
		event := NotificationEvent{
			Type:          backupNotificationType(observation),
			Namespace:     nativeJob.Namespace,
			ResourceKind:  "BackupPolicy",
			ResourceName:  nativeJob.Labels[policyNameLabel],
			Phase:         string(observation.Phase),
			Message:       observation.Message,
			SourceName:    source.Name,
			StorageName:   storage.Name,
			SnapshotName:  observation.SnapshotRef,
			NativeJobName: nativeJob.Name,
			Series:        series,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
		}
		if _, err := dispatchNotifications(ctx, r.Client, nativeJob.Namespace, refs, event); err == nil {
			base := nativeJob.DeepCopy()
			if nativeJob.Annotations == nil {
				nativeJob.Annotations = map[string]string{}
			}
			nativeJob.Annotations[notificationObservedAnnotation] = string(phase)
			if err := r.Patch(ctx, &nativeJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func parseNotificationAnnotation(value string) []string {
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimString(part)
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}

func (r *JobObserverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.Job{}).
		Complete(r)
}
