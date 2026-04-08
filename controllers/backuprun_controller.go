package controllers

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
		run.Status.Message = err.Error()
		run.Status.JobNames = nil
		run.Status.Storages = nil
		markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidSpec", err.Error(), run.Generation)
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidSpec", err.Error(), run.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	resolved, err := resolveBackupRunRefs(ctx, r.Client, &run)
	if err != nil {
		run.Status.JobNames = nil
		run.Status.Storages = nil
		switch {
		case isDependencyMissing(err):
			run.Status.Phase = dpv1alpha1.ResourcePhasePending
			run.Status.CompletedAt = nil
			run.Status.Message = err.Error()
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", err.Error(), run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(run.Status.Phase), run.Generation)
			return patchStatus(requeueSoon(), nil)
		case isPermanentDependencyError(err):
			run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			run.Status.CompletedAt = nowTime()
			run.Status.Message = err.Error()
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
		run.Status.Storages = nil
		message := fmt.Sprintf("referenced BackupSource %q is invalid: %v", resolved.Source.Name, err)
		run.Status.Message = message
		markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidSource", message, run.Generation)
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidSource", message, run.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}
	snapshot := strings.TrimSpace(run.Spec.Snapshot)
	if snapshot == "" {
		snapshot = run.Name
	}

	keepLast := retentionValue(dpv1alpha1.RetentionRule{})
	failedKeepLast := int32(0)
	if resolved.Policy != nil {
		resolvedKeepLast, _, err := resolveKeepLastRetention(ctx, r.Client, run.Namespace, resolved.Policy.Spec.Retention, localRefName(resolved.Policy.Spec.RetentionPolicyRef))
		if err != nil {
			run.Status.JobNames = nil
			run.Status.Storages = nil
			switch {
			case isDependencyMissing(err):
				run.Status.Phase = dpv1alpha1.ResourcePhasePending
				run.Status.CompletedAt = nil
				run.Status.Message = err.Error()
				markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", err.Error(), run.Generation)
				markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(run.Status.Phase), run.Generation)
				return patchStatus(requeueSoon(), nil)
			case isPermanentDependencyError(err):
				run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
				run.Status.CompletedAt = nowTime()
				run.Status.Message = err.Error()
				markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidRetentionPolicy", err.Error(), run.Generation)
				markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidRetentionPolicy", err.Error(), run.Generation)
				return patchStatus(ctrl.Result{}, nil)
			default:
				return ctrl.Result{}, err
			}
		}
		keepLast = resolvedKeepLast
		resolvedFailedKeepLast, _, err := resolveFailedRetention(ctx, r.Client, run.Namespace, localRefName(resolved.Policy.Spec.RetentionPolicyRef))
		if err != nil {
			run.Status.JobNames = nil
			run.Status.Storages = nil
			switch {
			case isDependencyMissing(err):
				run.Status.Phase = dpv1alpha1.ResourcePhasePending
				run.Status.CompletedAt = nil
				run.Status.Message = err.Error()
				markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "DependencyNotReady", err.Error(), run.Generation)
				markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "DependencyNotReady", phaseMessage(run.Status.Phase), run.Generation)
				return patchStatus(requeueSoon(), nil)
			case isPermanentDependencyError(err):
				run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
				run.Status.CompletedAt = nowTime()
				run.Status.Message = err.Error()
				markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "InvalidRetentionPolicy", err.Error(), run.Generation)
				markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "InvalidRetentionPolicy", err.Error(), run.Generation)
				return patchStatus(ctrl.Result{}, nil)
			default:
				return ctrl.Result{}, err
			}
		}
		failedKeepLast = resolvedFailedKeepLast
	}

	jobNames := make([]string, 0, len(resolved.Storages))
	storageStatuses := make([]dpv1alpha1.StorageRunStatus, 0, len(resolved.Storages))
	var latestCompletedAt *metav1.Time
	overallPhase := dpv1alpha1.ResourcePhaseSucceeded

	for i := range resolved.Storages {
		// A single BackupRun can fan out to multiple storages. We keep a
		// per-storage branch in status so operators can see partial success or
		// partial failure without reading every child Job by hand.
		storage := &resolved.Storages[i]
		snapshotName := dpv1alpha1.BuildSnapshotName(run.Name, storage.Name, "snapshot")
		desired, err := buildBackupRunJob(&run, resolved.Policy, resolved.Source, storage, resolved.StoragePath, snapshot, keepLast)
		if err != nil {
			run.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			run.Status.CompletedAt = nowTime()
			run.Status.Message = err.Error()
			run.Status.JobNames = uniqueStrings(jobNames)
			run.Status.Storages = storageStatuses
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
			run.Status.Storages = nil
			message := fmt.Sprintf("existing Job %q is not controlled by BackupRun %q", current.Name, run.Name)
			run.Status.Message = message
			markCondition(&run.Status.Conditions, "Accepted", metav1.ConditionFalse, "JobNameConflict", message, run.Generation)
			markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "JobNameConflict", message, run.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}

		storagePhase, message, completedAt := jobPhase(current)
		if completedAt != nil && (latestCompletedAt == nil || completedAt.After(latestCompletedAt.Time)) {
			latestCompletedAt = completedAt.DeepCopy()
		}
		storageSnapshot := ""
		storageSnapshotRef := ""
		if storagePhase == dpv1alpha1.ResourcePhaseSucceeded {
			storageSnapshot = snapshot
			storageSnapshotRef = snapshotName
		}
		storageStatuses = append(storageStatuses, dpv1alpha1.StorageRunStatus{
			Name:        storage.Name,
			Phase:       storagePhase,
			Message:     message,
			StoragePath: resolved.StoragePath,
			Snapshot:    storageSnapshot,
			SnapshotRef: storageSnapshotRef,
			UpdatedAt:   nowTime(),
		})
		overallPhase = combinePhases(overallPhase, storagePhase)

		if storagePhase == dpv1alpha1.ResourcePhaseSucceeded {
			if err := r.reconcileSnapshot(ctx, &run, resolved.Source, storage, resolved.StoragePath, snapshot, snapshotName, storagePhase, completedAt, message); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			if err := r.deleteSnapshotIfExists(ctx, run.Namespace, snapshotName); err != nil {
				return ctrl.Result{}, err
			}
		}
		if err := r.reconcileSnapshotSeries(ctx, &run, resolved.Source, storage, resolved.StoragePath, keepLast); err != nil {
			return ctrl.Result{}, err
		}
	}

	run.Status.JobNames = uniqueStrings(jobNames)
	run.Status.Storages = storageStatuses
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
		run.Status.Message = "all storage backup jobs completed successfully"
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionTrue, "AllJobsSucceeded", "all storage backup jobs completed successfully", run.Generation)
		result, err := patchStatus(ctrl.Result{}, nil)
		if err != nil {
			return result, err
		}
		return result, r.pruneScheduledRunHistory(ctx, &run, keepLast, failedKeepLast)
	case dpv1alpha1.ResourcePhaseFailed:
		run.Status.Message = "one or more storage backup jobs failed"
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "JobFailed", "one or more storage backup jobs failed", run.Generation)
		result, err := patchStatus(ctrl.Result{}, nil)
		if err != nil {
			return result, err
		}
		return result, r.pruneScheduledRunHistory(ctx, &run, keepLast, failedKeepLast)
	case dpv1alpha1.ResourcePhaseRunning:
		run.Status.Message = "backup jobs are still running"
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "Running", "backup jobs are still running", run.Generation)
	default:
		run.Status.Message = "backup jobs are pending scheduling or startup"
		markCondition(&run.Status.Conditions, "Completed", metav1.ConditionFalse, "Pending", "backup jobs are pending scheduling or startup", run.Generation)
	}
	return patchStatus(requeueSoon(), nil)
}

func (r *BackupRunReconciler) reconcileSnapshot(
	ctx context.Context,
	run *dpv1alpha1.BackupRun,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	snapshotValue string,
	snapshotName string,
	phase dpv1alpha1.ResourcePhase,
	completedAt *metav1.Time,
	message string,
) error {
	// Snapshot is our stable, restorable record of one backup artifact. It is
	// intentionally separate from the Job so users can restore by snapshot even
	// after Jobs age out.
	current := &dpv1alpha1.Snapshot{}
	key := client.ObjectKey{Namespace: run.Namespace, Name: snapshotName}
	err := r.Get(ctx, key, current)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		current = &dpv1alpha1.Snapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snapshotName,
				Namespace: run.Namespace,
			},
		}
	}

	original := current.DeepCopy()
	current.Labels = mergeStringMaps(current.Labels, managedResourceLabels("BackupRun", run.Name, "snapshot", source.Name, storage.Name))
	current.Spec = dpv1alpha1.SnapshotSpec{
		SourceRef:    corev1.LocalObjectReference{Name: source.Name},
		BackupRunRef: corev1.LocalObjectReference{Name: run.Name},
		StorageRef:   corev1.LocalObjectReference{Name: storage.Name},
		StoragePath:  storagePath,
		Driver:       source.Spec.Driver,
		Snapshot:     snapshotValue,
	}
	if err := controllerutil.SetControllerReference(run, current, r.Scheme); err != nil {
		return err
	}

	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, current); err != nil {
			return err
		}
	} else {
		if err := r.Patch(ctx, current, client.MergeFrom(original)); err != nil {
			return err
		}
	}

	statusOriginal := current.DeepCopy()
	current.Status.ObservedGeneration = current.Generation
	if current.Status.StartedAt == nil {
		if run.Status.StartedAt != nil {
			current.Status.StartedAt = run.Status.StartedAt.DeepCopy()
		} else {
			current.Status.StartedAt = nowTime()
		}
	}
	current.Status.Phase = phase
	current.Status.Message = message
	current.Status.ArtifactReady = phase == dpv1alpha1.ResourcePhaseSucceeded
	if phase == dpv1alpha1.ResourcePhaseSucceeded || phase == dpv1alpha1.ResourcePhaseFailed {
		if completedAt != nil {
			current.Status.CompletedAt = completedAt.DeepCopy()
		} else {
			current.Status.CompletedAt = nowTime()
		}
	} else {
		current.Status.CompletedAt = nil
	}

	conditionStatus := metav1.ConditionFalse
	reason := "Pending"
	switch phase {
	case dpv1alpha1.ResourcePhaseSucceeded:
		conditionStatus = metav1.ConditionTrue
		reason = "Ready"
	case dpv1alpha1.ResourcePhaseFailed:
		reason = "Failed"
	case dpv1alpha1.ResourcePhaseRunning:
		reason = "Running"
	}
	markCondition(&current.Status.Conditions, "Ready", conditionStatus, reason, message, current.Generation)
	return r.Status().Patch(ctx, current, client.MergeFrom(statusOriginal))
}

func (r *BackupRunReconciler) deleteSnapshotIfExists(ctx context.Context, namespace, snapshotName string) error {
	current := &dpv1alpha1.Snapshot{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: snapshotName}, current); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := r.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *BackupRunReconciler) reconcileSnapshotSeries(
	ctx context.Context,
	run *dpv1alpha1.BackupRun,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	keepLast int32,
) error {
	var snapshotList dpv1alpha1.SnapshotList
	if err := r.List(ctx, &snapshotList, client.InNamespace(run.Namespace)); err != nil {
		return err
	}

	series := make([]*dpv1alpha1.Snapshot, 0, len(snapshotList.Items))
	for i := range snapshotList.Items {
		snapshot := &snapshotList.Items[i]
		if snapshot.Spec.SourceRef.Name != source.Name {
			continue
		}
		if snapshot.Spec.StorageRef.Name != storage.Name {
			continue
		}
		if snapshot.Spec.StoragePath != storagePath {
			continue
		}
		series = append(series, snapshot)
	}
	if len(series) == 0 {
		return nil
	}

	sortSnapshotsNewestFirst(series)

	var latestSuccessful string
	successCount := int32(0)
	for _, snapshot := range series {
		if snapshot.Status.Phase == dpv1alpha1.ResourcePhaseSucceeded && latestSuccessful == "" {
			latestSuccessful = snapshot.Name
		}
	}

	for _, snapshot := range series {
		shouldDelete := false
		switch snapshot.Status.Phase {
		case dpv1alpha1.ResourcePhaseSucceeded:
			successCount++
			shouldDelete = keepLast > 0 && successCount > keepLast
		case dpv1alpha1.ResourcePhaseFailed:
			// Snapshot now represents a recoverable artifact. Failed backup
			// attempts stay on BackupRun status instead of being retained as
			// pseudo-snapshots.
			shouldDelete = true
		}
		if shouldDelete {
			if err := r.Delete(ctx, snapshot); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			continue
		}

		statusOriginal := snapshot.DeepCopy()
		snapshot.Status.Latest = latestSuccessful != "" && snapshot.Name == latestSuccessful
		snapshot.Status.ArtifactReady = snapshot.Status.Phase == dpv1alpha1.ResourcePhaseSucceeded
		if snapshot.Status.Message == "" {
			snapshot.Status.Message = phaseMessage(snapshot.Status.Phase)
		}
		if err := r.Status().Patch(ctx, snapshot, client.MergeFrom(statusOriginal)); err != nil {
			return err
		}
	}

	return nil
}

func (r *BackupRunReconciler) pruneScheduledRunHistory(
	ctx context.Context,
	run *dpv1alpha1.BackupRun,
	successKeepLast int32,
	failedKeepLast int32,
) error {
	if run.Labels["dataprotection.archinfra.io/triggered-by"] != "cronjob" {
		return nil
	}
	policyName := strings.TrimSpace(run.Labels["dataprotection.archinfra.io/policy-name"])
	storageName := strings.TrimSpace(run.Labels["dataprotection.archinfra.io/storage-name"])
	if policyName == "" || storageName == "" {
		return nil
	}

	var runList dpv1alpha1.BackupRunList
	if err := r.List(ctx, &runList,
		client.InNamespace(run.Namespace),
		client.MatchingLabels{
			"dataprotection.archinfra.io/triggered-by": "cronjob",
			"dataprotection.archinfra.io/policy-name":  policyName,
			"dataprotection.archinfra.io/storage-name": storageName,
		},
	); err != nil {
		return err
	}

	series := make([]*dpv1alpha1.BackupRun, 0, len(runList.Items))
	for i := range runList.Items {
		candidate := &runList.Items[i]
		if candidate.DeletionTimestamp != nil {
			continue
		}
		if !isTerminalBackupRun(candidate.Status.Phase) {
			continue
		}
		series = append(series, candidate)
	}
	if len(series) == 0 {
		return nil
	}

	sortBackupRunsNewestFirst(series)

	successCount := int32(0)
	failedCount := int32(0)
	for _, candidate := range series {
		shouldDelete := false
		switch candidate.Status.Phase {
		case dpv1alpha1.ResourcePhaseSucceeded:
			successCount++
			shouldDelete = successKeepLast > 0 && successCount > successKeepLast
		case dpv1alpha1.ResourcePhaseFailed:
			if failedKeepLast <= 0 {
				shouldDelete = true
			} else {
				failedCount++
				shouldDelete = failedCount > failedKeepLast
			}
		}
		if !shouldDelete {
			continue
		}
		if err := r.Delete(ctx, candidate); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
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
