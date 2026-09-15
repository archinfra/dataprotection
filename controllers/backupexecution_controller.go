package controllers

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupExecutionReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	APIReader client.Reader
}

func (r *BackupExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var execution dpv1alpha1.BackupExecution
	if err := r.Get(ctx, req.NamespacedName, &execution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := execution.DeepCopy()
	execution.Status.ObservedGeneration = execution.Generation
	if execution.Status.StartedAt == nil {
		execution.Status.StartedAt = nowTime()
	}

	if err := execution.Spec.ValidateBasic(); err != nil {
		execution.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		execution.Status.Message = err.Error()
		markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", execution.Status.Message, execution.Generation)
		if err := r.Status().Patch(ctx, &execution, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	resolved, err := resolveBackupExecutionDependencies(ctx, r.Client, &execution)
	if err != nil {
		execution.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		execution.Status.Message = err.Error()
		markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "DependencyError", execution.Status.Message, execution.Generation)
		if err := r.Status().Patch(ctx, &execution, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	execution.Status.Series = resolved.Series
	notificationRefs := resolveJobNotificationRefs(execution.Spec.NotificationRefs, resolved.Policy)
	if resolved.Source.Spec.Paused {
		execution.Status.Phase = dpv1alpha1.ResourcePhasePaused
		execution.Status.Message = "backup source is paused"
		markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "Paused", execution.Status.Message, execution.Generation)
		if err := r.Status().Patch(ctx, &execution, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	adoptedNativeName := trimString(execution.Annotations[adoptedNativeJobAnnotation])
	nativeName := adoptedNativeName
	if nativeName == "" {
		nativeName = dpv1alpha1.BuildJobName(execution.Name, "backup")
	}
	execution.Status.NativeJobName = nativeName

	nativeJob := &batchv1.Job{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: execution.Namespace, Name: nativeName}, nativeJob); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if adoptedNativeName != "" {
			execution.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			execution.Status.Message = "adopted backup native job not found"
			markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "NativeJobNotFound", execution.Status.Message, execution.Generation)
			if err := r.Status().Patch(ctx, &execution, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		rendered, err := buildBackupExecutionNativeJob(&execution, resolved)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := controllerutil.SetControllerReference(&execution, rendered, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, rendered); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		execution.Status.Phase = dpv1alpha1.ResourcePhaseRunning
		execution.Status.Message = "backup native job created"
		markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "Running", execution.Status.Message, execution.Generation)
		if err := r.Status().Patch(ctx, &execution, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	if nativeJob.Status.StartTime != nil {
		execution.Status.StartedAt = nativeJob.Status.StartTime
	}
	if observation, err := observeTerminalBackupExecution(ctx, r.Client, r.APIReader, nativeJob, &execution, resolved); err != nil {
		return ctrl.Result{}, err
	} else if observation != nil {
		execution.Status.Phase = observation.Phase
		execution.Status.Message = observation.Message
		execution.Status.CompletedAt = observation.CompletedAt
		execution.Status.StorageProbeResult = observation.StorageProbeResult
		execution.Status.StorageProbeMessage = observation.StorageProbeMessage
		execution.Status.SnapshotRef = observation.SnapshotRef
		if len(notificationRefs) > 0 && execution.Status.Notification.Phase != dpv1alpha1.NotificationDeliverySucceeded {
			event := NotificationEvent{
				Type:          backupNotificationType(observation),
				Namespace:     execution.Namespace,
				ResourceKind:  "BackupExecution",
				ResourceName:  execution.Name,
				Phase:         string(observation.Phase),
				Message:       observation.Message,
				SourceName:    resolved.Source.Name,
				StorageName:   resolved.Storage.Name,
				SnapshotName:  observation.SnapshotRef,
				NativeJobName: nativeJob.Name,
				Series:        resolved.Series,
				Timestamp:     nowTime().Time.Format(time.RFC3339),
			}
			execution.Status.Notification, _ = dispatchNotifications(ctx, r.Client, execution.Namespace, notificationRefs, event)
		}
		if observation.Phase == dpv1alpha1.ResourcePhaseSucceeded {
			markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionTrue, "Completed", execution.Status.Message, execution.Generation)
		} else {
			markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "Failed", execution.Status.Message, execution.Generation)
		}
	} else if nativeJob.Status.Active > 0 || nativeJob.Status.StartTime != nil {
		execution.Status.Phase = dpv1alpha1.ResourcePhaseRunning
		execution.Status.Message = "backup native job is running"
		markCondition(&execution.Status.Conditions, "Ready", metav1.ConditionFalse, "Running", execution.Status.Message, execution.Generation)
	} else {
		execution.Status.Phase = dpv1alpha1.ResourcePhasePending
		execution.Status.Message = "waiting for backup native job to start"
	}

	if err := r.Status().Patch(ctx, &execution, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if execution.Status.Phase == dpv1alpha1.ResourcePhaseSucceeded || execution.Status.Phase == dpv1alpha1.ResourcePhaseFailed {
		return ctrl.Result{}, nil
	}
	return requeueSoon(), nil
}

func (r *BackupExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupExecution{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
