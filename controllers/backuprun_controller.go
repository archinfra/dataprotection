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

type BackupRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("backupRun", req.NamespacedName.String())

	var run dpv1alpha1.BackupRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := run.DeepCopy()
	run.Status.ObservedGeneration = run.Generation
	if run.Status.StartedAt == nil {
		run.Status.StartedAt = nowTime()
	}

	patchStatus := func(result ctrl.Result, reconcileErr error) (ctrl.Result, error) {
		if err := r.Status().Patch(ctx, &run, client.MergeFrom(original)); err != nil {
			logger.Error(err, "unable to patch BackupRun status")
			return ctrl.Result{}, err
		}
		return result, reconcileErr
	}

	if err := run.Spec.ValidateBasic(); err != nil {
		run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		run.Status.CompletedAt = nowTime()
		run.Status.JobNames = nil
		run.Status.Repositories = nil
		markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidSpec", err.Error(), run.Generation)
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidSpec", err.Error(), run.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	resolved, err := resolveBackupRunRefs(ctx, r.Client, &run)
	if err != nil {
		run.Status.JobNames = nil
		run.Status.Repositories = nil
		switch {
		case isDependencyMissing(err):
			run.Status.Phase = dpv1alpha1.ResourcePhasePending
			run.Status.CompletedAt = nil
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", err.Error(), run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(run.Status.Phase), run.Generation)
			return patchStatus(requeueSoon(), nil)
		case isPermanentDependencyError(err):
			run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			run.Status.CompletedAt = nowTime()
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidReference", err.Error(), run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidReference", err.Error(), run.Generation)
			return patchStatus(ctrl.Result{}, nil)
		default:
			return ctrl.Result{}, err
		}
	}

	if err := resolved.Source.Spec.ValidateBasic(); err != nil {
		run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		run.Status.CompletedAt = nowTime()
		run.Status.JobNames = nil
		run.Status.Repositories = nil
		message := fmt.Sprintf("referenced BackupSource %q is invalid: %v", resolved.Source.Name, err)
		markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidSource", message, run.Generation)
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidSource", message, run.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}
	for _, repository := range resolved.Repositories {
		if err := repository.Spec.ValidateBasic(); err != nil {
			run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			run.Status.CompletedAt = nowTime()
			run.Status.JobNames = nil
			run.Status.Repositories = nil
			message := fmt.Sprintf("referenced BackupRepository %q is invalid: %v", repository.Name, err)
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidRepository", message, run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidRepository", message, run.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}
	}

	snapshot := strings.TrimSpace(run.Spec.Snapshot)
	if snapshot == "" {
		snapshot = run.Name
	}

	jobNames := make([]string, 0, len(resolved.Repositories))
	repositoryStatuses := make([]dpv1alpha1.RepositoryRunStatus, 0, len(resolved.Repositories))
	var latestCompletedAt *metav1.Time
	overallPhase := dpv1alpha1.ResourcePhaseSucceeded

	for i := range resolved.Repositories {
		repository := &resolved.Repositories[i]
		desired, err := buildBackupRunJob(&run, resolved.Policy, resolved.Source, repository, snapshot)
		if err != nil {
			run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			run.Status.CompletedAt = nowTime()
			run.Status.JobNames = uniqueStrings(jobNames)
			run.Status.Repositories = repositoryStatuses
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "RenderFailed", err.Error(), run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "RenderFailed", err.Error(), run.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}
		jobNames = append(jobNames, desired.Name)

		current := &batchv1.Job{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			current = desired.DeepCopy()
			if err := controllerutil.SetControllerReference(&run, current, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, current); err != nil {
				return ctrl.Result{}, err
			}
		} else if !metav1.IsControlledBy(current, &run) {
			run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			run.Status.CompletedAt = nowTime()
			run.Status.JobNames = uniqueStrings(jobNames)
			run.Status.Repositories = nil
			message := fmt.Sprintf("existing Job %q is not controlled by BackupRun %q", current.Name, run.Name)
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "JobNameConflict", message, run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "JobNameConflict", message, run.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}

		repositoryPhase, message, completedAt := jobPhase(current)
		if completedAt != nil && (latestCompletedAt == nil || completedAt.After(latestCompletedAt.Time)) {
			latestCompletedAt = completedAt.DeepCopy()
		}
		repositoryStatuses = append(repositoryStatuses, dpv1alpha1.RepositoryRunStatus{
			Name:      repository.Name,
			Phase:     repositoryPhase,
			Message:   message,
			Snapshot:  snapshot,
			UpdatedAt: nowTime(),
		})
		overallPhase = combinePhases(overallPhase, repositoryPhase)
	}

	run.Status.JobNames = uniqueStrings(jobNames)
	run.Status.Repositories = repositoryStatuses
	run.Status.Phase = overallPhase
	if overallPhase == dpv1alpha1.ResourcePhaseSucceeded || overallPhase == dpv1alpha1.ResourcePhaseFailed {
		if latestCompletedAt == nil {
			latestCompletedAt = nowTime()
		}
		run.Status.CompletedAt = latestCompletedAt
	} else {
		run.Status.CompletedAt = nil
	}

	markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionTrue, "Reconciled", fmt.Sprintf("managed %d Job resource(s)", len(jobNames)), run.Generation)
	switch overallPhase {
	case dpv1alpha1.ResourcePhaseSucceeded:
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionTrue, "AllJobsSucceeded", "all repository backup jobs completed successfully", run.Generation)
		return patchStatus(ctrl.Result{}, nil)
	case dpv1alpha1.ResourcePhaseFailed:
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "JobFailed", "one or more repository backup jobs failed", run.Generation)
		return patchStatus(ctrl.Result{}, nil)
	case dpv1alpha1.ResourcePhaseRunning:
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "Running", "backup jobs are still running", run.Generation)
	default:
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "Pending", "backup jobs are pending scheduling or startup", run.Generation)
	}
	return patchStatus(requeueSoon(), nil)
}

func combinePhases(current, next dpv1alpha1.ResourcePhase) dpv1alpha1.ResourcePhase {
	if current == dpv1alpha1.ResourcePhaseFailed || next == dpv1alpha1.ResourcePhaseFailed {
		return dpv1alpha1.ResourcePhaseFailed
	}
	if current == dpv1alpha1.ResourcePhaseRunning || next == dpv1alpha1.ResourcePhaseRunning {
		return dpv1alpha1.ResourcePhaseRunning
	}
	if current == dpv1alpha1.ResourcePhasePending || next == dpv1alpha1.ResourcePhasePending {
		return dpv1alpha1.ResourcePhasePending
	}
	if current == "" {
		return next
	}
	return current
}

func (r *BackupRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupRun{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
