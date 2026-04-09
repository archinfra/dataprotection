package controllers

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupJobReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	APIReader client.Reader
}

func (r *BackupJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var backupJob dpv1alpha1.BackupJob
	if err := r.Get(ctx, req.NamespacedName, &backupJob); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := backupJob.DeepCopy()
	backupJob.Status.ObservedGeneration = backupJob.Generation
	if backupJob.Status.StartedAt == nil {
		backupJob.Status.StartedAt = nowTime()
	}

	if err := backupJob.Spec.ValidateBasic(); err != nil {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		backupJob.Status.Message = err.Error()
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	resolved, err := resolveBackupJobDependencies(ctx, r.Client, &backupJob)
	if err != nil {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		backupJob.Status.Message = err.Error()
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "DependencyError", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	backupJob.Status.Series = resolved.Series
	notificationRefs := resolveJobNotificationRefs(backupJob.Spec.NotificationRefs, resolved.Policy)
	if resolved.Source.Spec.Paused {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhasePaused
		backupJob.Status.Message = "backup source is paused"
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Paused", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	nativeName := dpv1alpha1.BuildJobName(backupJob.Name, "backup")
	backupJob.Status.NativeJobName = nativeName
	nativeJob := &batchv1.Job{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: backupJob.Namespace, Name: nativeName}, nativeJob); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		rendered, err := buildManualBackupNativeJob(&backupJob, resolved)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := controllerutil.SetControllerReference(&backupJob, rendered, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, rendered); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		backupJob.Status.Phase = dpv1alpha1.ResourcePhaseRunning
		backupJob.Status.Message = "backup native job created"
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Running", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	backupJob.Status.StartedAt = nativeJob.Status.StartTime
	if observation, err := observeTerminalBackupJob(
		ctx,
		r.Client,
		r.APIReader,
		nativeJob,
		resolved.Source,
		resolved.Storage,
		func() *corev1.LocalObjectReference {
			if resolved.Policy == nil {
				return nil
			}
			return &corev1.LocalObjectReference{Name: resolved.Policy.Name}
		}(),
		&corev1.LocalObjectReference{Name: backupJob.Name},
		resolved.Series,
		resolved.KeepLast,
	); err != nil {
		return ctrl.Result{}, err
	} else if observation != nil {
		backupJob.Status.Phase = observation.Phase
		backupJob.Status.Message = observation.Message
		backupJob.Status.CompletedAt = observation.CompletedAt
		backupJob.Status.StorageProbeResult = observation.StorageProbeResult
		backupJob.Status.StorageProbeMessage = observation.StorageProbeMessage
		backupJob.Status.SnapshotRef = observation.SnapshotRef
		if len(notificationRefs) > 0 && backupJob.Status.Notification.Phase != dpv1alpha1.NotificationDeliverySucceeded {
			event := NotificationEvent{
				Type:          backupNotificationType(observation),
				Namespace:     backupJob.Namespace,
				ResourceKind:  "BackupJob",
				ResourceName:  backupJob.Name,
				Phase:         string(observation.Phase),
				Message:       observation.Message,
				SourceName:    resolved.Source.Name,
				StorageName:   resolved.Storage.Name,
				SnapshotName:  observation.SnapshotRef,
				NativeJobName: nativeJob.Name,
				Series:        resolved.Series,
				Timestamp:     nowTime().Time.Format(time.RFC3339),
			}
			backupJob.Status.Notification, _ = dispatchNotifications(ctx, r.Client, backupJob.Namespace, notificationRefs, event)
		}
		if observation.Phase == dpv1alpha1.ResourcePhaseSucceeded {
			markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionTrue, "Completed", backupJob.Status.Message, backupJob.Generation)
		} else {
			markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Failed", backupJob.Status.Message, backupJob.Generation)
		}
	} else if nativeJob.Status.Active > 0 || nativeJob.Status.StartTime != nil {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhaseRunning
		backupJob.Status.Message = "backup native job is running"
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Running", backupJob.Status.Message, backupJob.Generation)
	} else {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhasePending
		backupJob.Status.Message = "waiting for backup native job to start"
	}

	if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return requeueSoon(), nil
}

func (r *BackupJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupJob{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
