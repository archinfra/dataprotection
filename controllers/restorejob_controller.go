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

type RestoreJobReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	APIReader client.Reader
}

func (r *RestoreJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var restoreJob dpv1alpha1.RestoreJob
	if err := r.Get(ctx, req.NamespacedName, &restoreJob); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := restoreJob.DeepCopy()
	restoreJob.Status.ObservedGeneration = restoreJob.Generation
	if restoreJob.Status.StartedAt == nil {
		restoreJob.Status.StartedAt = nowTime()
	}

	if err := restoreJob.Spec.ValidateBasic(); err != nil {
		restoreJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restoreJob.Status.Message = err.Error()
		markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", restoreJob.Status.Message, restoreJob.Generation)
		if err := r.Status().Patch(ctx, &restoreJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	resolved, err := resolveRestoreJobDependencies(ctx, r.Client, &restoreJob)
	if err != nil {
		restoreJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restoreJob.Status.Message = err.Error()
		markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionFalse, "DependencyError", restoreJob.Status.Message, restoreJob.Generation)
		if err := r.Status().Patch(ctx, &restoreJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}
	if resolved.Snapshot.Status.Phase != dpv1alpha1.ResourcePhaseSucceeded || !resolved.Snapshot.Status.ArtifactReady {
		restoreJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restoreJob.Status.Message = "snapshot is not ready for restore"
		markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionFalse, "SnapshotNotReady", restoreJob.Status.Message, restoreJob.Generation)
		if err := r.Status().Patch(ctx, &restoreJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	nativeName := dpv1alpha1.BuildJobName(restoreJob.Name, "restore")
	restoreJob.Status.NativeJobName = nativeName
	nativeJob := &batchv1.Job{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: restoreJob.Namespace, Name: nativeName}, nativeJob); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		rendered, err := buildRestoreNativeJob(&restoreJob, resolved)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := controllerutil.SetControllerReference(&restoreJob, rendered, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, rendered); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		restoreJob.Status.Phase = dpv1alpha1.ResourcePhaseRunning
		restoreJob.Status.Message = "restore native job created"
		markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Running", restoreJob.Status.Message, restoreJob.Generation)
		if err := r.Status().Patch(ctx, &restoreJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	restoreJob.Status.StartedAt = nativeJob.Status.StartTime
	if observation, err := observeTerminalRestoreJob(ctx, r.Client, r.APIReader, nativeJob, resolved.Storage); err != nil {
		return ctrl.Result{}, err
	} else if observation != nil {
		restoreJob.Status.Phase = observation.Phase
		restoreJob.Status.Message = observation.Message
		restoreJob.Status.CompletedAt = observation.CompletedAt
		restoreJob.Status.StorageProbeResult = observation.StorageProbeResult
		restoreJob.Status.StorageProbeMessage = observation.StorageProbeMessage
		if len(restoreJob.Spec.NotificationRefs) > 0 && restoreJob.Status.Notification.Phase != dpv1alpha1.NotificationDeliverySucceeded {
			event := NotificationEvent{
				Type:          restoreNotificationType(observation),
				Namespace:     restoreJob.Namespace,
				ResourceKind:  "RestoreJob",
				ResourceName:  restoreJob.Name,
				Phase:         string(observation.Phase),
				Message:       observation.Message,
				SourceName:    resolved.Source.Name,
				StorageName:   resolved.Storage.Name,
				SnapshotName:  resolved.Snapshot.Name,
				NativeJobName: nativeJob.Name,
				Series:        resolved.Snapshot.Spec.Series,
				Timestamp:     nowTime().Time.Format(time.RFC3339),
			}
			restoreJob.Status.Notification, _ = dispatchNotifications(ctx, r.Client, restoreJob.Namespace, restoreJob.Spec.NotificationRefs, event)
		}
		if observation.Phase == dpv1alpha1.ResourcePhaseSucceeded {
			markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionTrue, "Completed", restoreJob.Status.Message, restoreJob.Generation)
		} else {
			markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Failed", restoreJob.Status.Message, restoreJob.Generation)
		}
	} else if nativeJob.Status.Active > 0 || nativeJob.Status.StartTime != nil {
		restoreJob.Status.Phase = dpv1alpha1.ResourcePhaseRunning
		restoreJob.Status.Message = "restore native job is running"
		markCondition(&restoreJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Running", restoreJob.Status.Message, restoreJob.Generation)
	} else {
		restoreJob.Status.Phase = dpv1alpha1.ResourcePhasePending
		restoreJob.Status.Message = "waiting for restore native job to start"
	}

	if err := r.Status().Patch(ctx, &restoreJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return requeueSoon(), nil
}

func (r *RestoreJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.RestoreJob{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
