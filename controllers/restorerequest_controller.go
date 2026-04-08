package controllers

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type RestoreRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *RestoreRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("restoreRequest", req.NamespacedName.String())

	var restore dpv1alpha1.RestoreRequest
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := restore.DeepCopy()
	restore.Status.ObservedGeneration = restore.Generation
	if restore.Status.StartedAt == nil {
		restore.Status.StartedAt = nowTime()
	}

	patchStatus := func(result ctrl.Result, reconcileErr error) (ctrl.Result, error) {
		if err := r.Status().Patch(ctx, &restore, client.MergeFrom(original)); err != nil {
			logger.Error(err, "unable to patch RestoreRequest status")
			return ctrl.Result{}, err
		}
		return result, reconcileErr
	}

	if err := restore.Spec.ValidateBasic(); err != nil {
		restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restore.Status.CompletedAt = nowTime()
		restore.Status.Message = err.Error()
		restore.Status.JobName = ""
		markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidSpec", err.Error(), restore.Generation)
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidSpec", err.Error(), restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	source, err := getBackupSource(ctx, r.Client, restore.Namespace, restore.Spec.SourceRef.Name)
	if err != nil {
		switch {
		case isDependencyMissing(err):
			restore.Status.Phase = dpv1alpha1.ResourcePhasePending
			restore.Status.CompletedAt = nil
			restore.Status.Message = fmt.Sprintf("unable to resolve BackupSource %q: %v", restore.Spec.SourceRef.Name, err)
			restore.Status.JobName = ""
			markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", fmt.Sprintf("unable to resolve BackupSource %q: %v", restore.Spec.SourceRef.Name, err), restore.Generation)
			markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(restore.Status.Phase), restore.Generation)
			return patchStatus(requeueSoon(), nil)
		default:
			return ctrl.Result{}, err
		}
	}
	if err := source.Spec.ValidateBasic(); err != nil {
		restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restore.Status.CompletedAt = nowTime()
		message := fmt.Sprintf("referenced BackupSource %q is invalid: %v", source.Name, err)
		restore.Status.Message = message
		restore.Status.JobName = ""
		markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidSource", message, restore.Generation)
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidSource", message, restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	// Restore prefers Snapshot as the primary input because Snapshot already
	// captures both the storage identity and the effective storage path.
	backupRun, snapshotRef, storage, storagePath, err := resolveRestoreStorage(ctx, r.Client, &restore, source)
	if err != nil {
		restore.Status.JobName = ""
		switch {
		case isDependencyMissing(err):
			restore.Status.Phase = dpv1alpha1.ResourcePhasePending
			restore.Status.CompletedAt = nil
			restore.Status.Message = err.Error()
			markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", err.Error(), restore.Generation)
			markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(restore.Status.Phase), restore.Generation)
			return patchStatus(requeueSoon(), nil)
		case isPermanentDependencyError(err):
			restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			restore.Status.CompletedAt = nowTime()
			restore.Status.Message = err.Error()
			markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidReference", err.Error(), restore.Generation)
			markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidReference", err.Error(), restore.Generation)
			return patchStatus(ctrl.Result{}, nil)
		default:
			return ctrl.Result{}, err
		}
	}
	if snapshotRef != nil {
		if snapshotRef.Spec.SourceRef.Name != restore.Spec.SourceRef.Name {
			restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			restore.Status.CompletedAt = nowTime()
			message := fmt.Sprintf("spec.sourceRef.name %q does not match snapshot %q sourceRef %q", restore.Spec.SourceRef.Name, snapshotRef.Name, snapshotRef.Spec.SourceRef.Name)
			restore.Status.Message = message
			restore.Status.JobName = ""
			markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidReference", message, restore.Generation)
			markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidReference", message, restore.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}
	}

	if backupRun != nil && backupRun.Spec.SourceRef.Name != restore.Spec.SourceRef.Name {
		restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restore.Status.CompletedAt = nowTime()
		message := fmt.Sprintf("spec.sourceRef.name %q does not match backupRun %q sourceRef %q", restore.Spec.SourceRef.Name, backupRun.Name, backupRun.Spec.SourceRef.Name)
		restore.Status.Message = message
		restore.Status.JobName = ""
		markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidReference", message, restore.Generation)
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidReference", message, restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	snapshot := strings.TrimSpace(restore.Spec.Snapshot)
	if snapshot == "" && snapshotRef != nil {
		snapshot = strings.TrimSpace(snapshotRef.Spec.Snapshot)
	}
	if snapshot == "" && backupRun != nil {
		for _, storageStatus := range backupRun.Status.Storages {
			if storageStatus.Name == storage.Name && strings.TrimSpace(storageStatus.Snapshot) != "" {
				snapshot = storageStatus.Snapshot
				break
			}
		}
		if snapshot == "" {
			snapshot = strings.TrimSpace(backupRun.Spec.Snapshot)
		}
		if snapshot == "" {
			snapshot = backupRun.Name
		}
	}
	if snapshot == "" {
		snapshot = restore.Name
	}

	execution := dpv1alpha1.ExecutionTemplateSpec{}
	if backupRun != nil && backupRun.Spec.PolicyRef != nil && strings.TrimSpace(backupRun.Spec.PolicyRef.Name) != "" {
		policy, err := getBackupPolicy(ctx, r.Client, restore.Namespace, backupRun.Spec.PolicyRef.Name)
		if err != nil {
			if isDependencyMissing(err) {
				restore.Status.Phase = dpv1alpha1.ResourcePhasePending
				restore.Status.CompletedAt = nil
				restore.Status.Message = fmt.Sprintf("referenced BackupPolicy %q is not ready: %v", backupRun.Spec.PolicyRef.Name, err)
				restore.Status.JobName = ""
				markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", fmt.Sprintf("referenced BackupPolicy %q is not ready: %v", backupRun.Spec.PolicyRef.Name, err), restore.Generation)
				markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(restore.Status.Phase), restore.Generation)
				return patchStatus(requeueSoon(), nil)
			}
			return ctrl.Result{}, err
		}
		execution = policy.Spec.Execution
	}

	desired, err := buildRestoreJob(&restore, backupRun, source, storage, storagePath, execution, snapshot)
	if err != nil {
		restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restore.Status.CompletedAt = nowTime()
		restore.Status.Message = err.Error()
		restore.Status.JobName = ""
		markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "RenderFailed", err.Error(), restore.Generation)
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "RenderFailed", err.Error(), restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}
	restore.Status.JobName = desired.Name

	current := &batchv1.Job{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		current = desired.DeepCopy()
		if err := controllerutil.SetControllerReference(&restore, current, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, current); err != nil {
			return ctrl.Result{}, err
		}
	} else if !metav1.IsControlledBy(current, &restore) {
		restore.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		restore.Status.CompletedAt = nowTime()
		message := fmt.Sprintf("existing Job %q is not controlled by RestoreRequest %q", current.Name, restore.Name)
		restore.Status.Message = message
		markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionFalse, "JobNameConflict", message, restore.Generation)
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "JobNameConflict", message, restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	jobExecutionPhase, message, completedAt := jobPhase(current)
	restore.Status.Phase = jobExecutionPhase
	restore.Status.Message = message
	if jobExecutionPhase == dpv1alpha1.ResourcePhaseSucceeded || jobExecutionPhase == dpv1alpha1.ResourcePhaseFailed {
		if completedAt == nil {
			completedAt = nowTime()
		}
		restore.Status.CompletedAt = completedAt
	} else {
		restore.Status.CompletedAt = nil
	}

	markCondition(&restore.Status.Conditions, "Accepted", metav1.ConditionTrue, "Reconciled", fmt.Sprintf("managed restore Job %q", restore.Status.JobName), restore.Generation)
	switch jobExecutionPhase {
	case dpv1alpha1.ResourcePhaseSucceeded:
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionTrue, "JobSucceeded", message, restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	case dpv1alpha1.ResourcePhaseFailed:
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "JobFailed", message, restore.Generation)
		return patchStatus(ctrl.Result{}, nil)
	case dpv1alpha1.ResourcePhaseRunning:
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "Running", message, restore.Generation)
	default:
		markCondition(&restore.Status.Conditions, "Completed", metav1.ConditionFalse, "Pending", message, restore.Generation)
	}

	return patchStatus(requeueSoon(), nil)
}

func (r *RestoreRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.RestoreRequest{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
