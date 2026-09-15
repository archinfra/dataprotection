package controllers

import (
	"context"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

// BackupJobReconciler is a compatibility adapter. BackupJob no longer executes
// native Jobs directly; it delegates every request to BackupExecution.
type BackupJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var backupJob dpv1alpha1.BackupJob
	if err := r.Get(ctx, req.NamespacedName, &backupJob); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := backupJob.DeepCopy()
	backupJob.Status.ObservedGeneration = backupJob.Generation
	if err := backupJob.Spec.ValidateBasic(); err != nil {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		backupJob.Status.Message = err.Error()
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	desiredSpec := dpv1alpha1.BackupExecutionSpec{
		PolicyRef:        backupJob.Spec.PolicyRef,
		SourceRef:        backupJob.Spec.SourceRef,
		StorageRef:       backupJob.Spec.StorageRef,
		RetentionRef:     backupJob.Spec.RetentionRef,
		NotificationRefs: backupJob.Spec.NotificationRefs,
		JobRuntime:       backupJob.Spec.JobRuntime,
		SnapshotName:     backupJob.Spec.SnapshotName,
		Reason:           backupJob.Spec.Reason,
		Trigger:          dpv1alpha1.BackupExecutionTriggerManual,
	}

	var execution dpv1alpha1.BackupExecution
	key := client.ObjectKey{Namespace: backupJob.Namespace, Name: backupJob.Name}
	if err := r.Get(ctx, key, &execution); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		execution = dpv1alpha1.BackupExecution{
			ObjectMeta: metav1.ObjectMeta{Name: backupJob.Name, Namespace: backupJob.Namespace},
			Spec:       desiredSpec,
		}
		if err := controllerutil.SetControllerReference(&backupJob, &execution, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, &execution); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		backupJob.Status.Phase = dpv1alpha1.ResourcePhasePending
		backupJob.Status.Message = "delegated to BackupExecution"
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "Delegated", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	owner := metav1.GetControllerOf(&execution)
	if owner == nil || owner.UID != backupJob.UID {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		backupJob.Status.Message = "BackupExecution with the same name is not owned by this legacy BackupJob"
		markCondition(&backupJob.Status.Conditions, "Ready", metav1.ConditionFalse, "ExecutionConflict", backupJob.Status.Message, backupJob.Generation)
		if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !reflect.DeepEqual(execution.Spec, desiredSpec) {
		execBase := execution.DeepCopy()
		execution.Spec = desiredSpec
		if err := r.Patch(ctx, &execution, client.MergeFrom(execBase)); err != nil {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	backupJob.Status.Phase = execution.Status.Phase
	backupJob.Status.StartedAt = execution.Status.StartedAt
	backupJob.Status.CompletedAt = execution.Status.CompletedAt
	backupJob.Status.Message = execution.Status.Message
	backupJob.Status.NativeJobName = execution.Status.NativeJobName
	backupJob.Status.Series = execution.Status.Series
	backupJob.Status.SnapshotRef = execution.Status.SnapshotRef
	backupJob.Status.StorageProbeResult = execution.Status.StorageProbeResult
	backupJob.Status.StorageProbeMessage = execution.Status.StorageProbeMessage
	backupJob.Status.Notification = execution.Status.Notification
	backupJob.Status.Conditions = execution.Status.Conditions
	if backupJob.Status.Phase == "" {
		backupJob.Status.Phase = dpv1alpha1.ResourcePhasePending
		backupJob.Status.Message = "waiting for BackupExecution"
	}

	if err := r.Status().Patch(ctx, &backupJob, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return requeueSoon(), nil
}

func (r *BackupJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupJob{}).
		Owns(&dpv1alpha1.BackupExecution{}).
		Complete(r)
}
