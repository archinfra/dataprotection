package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type terminalBackupObservation struct {
	Phase               dpv1alpha1.ResourcePhase
	Message             string
	CompletedAt         *metav1.Time
	StorageProbeResult  dpv1alpha1.ProbeResult
	StorageProbeMessage string
	SnapshotRef         string
}

type terminalRestoreObservation struct {
	Phase               dpv1alpha1.ResourcePhase
	Message             string
	CompletedAt         *metav1.Time
	StorageProbeResult  dpv1alpha1.ProbeResult
	StorageProbeMessage string
}

func jobTerminalState(job *batchv1.Job) (dpv1alpha1.ResourcePhase, string, bool) {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			message := trimString(condition.Message)
			if message == "" {
				message = "job completed successfully"
			}
			return dpv1alpha1.ResourcePhaseSucceeded, message, true
		}
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			message := trimString(condition.Message)
			if message == "" {
				message = "job execution failed"
			}
			return dpv1alpha1.ResourcePhaseFailed, message, true
		}
	}
	return "", "", false
}

func jobCompletedAt(job *batchv1.Job) *metav1.Time {
	var latest *metav1.Time
	for _, condition := range job.Status.Conditions {
		if condition.Type != batchv1.JobComplete && condition.Type != batchv1.JobFailed {
			continue
		}
		if latest == nil || condition.LastTransitionTime.After(latest.Time) {
			ts := condition.LastTransitionTime
			latest = &ts
		}
	}
	return latest
}

func observeTerminalBackupJob(ctx context.Context, c client.Client, reader client.Reader, job *batchv1.Job, source *dpv1alpha1.BackupSource, storage *dpv1alpha1.BackupStorage, policyRef, backupJobRef *corev1.LocalObjectReference, series string, keepLast int32) (*terminalBackupObservation, error) {
	phase, message, done := jobTerminalState(job)
	if !done {
		return nil, nil
	}
	completedAt := jobCompletedAt(job)
	observation := &terminalBackupObservation{
		Phase:       phase,
		Message:     message,
		CompletedAt: completedAt,
	}

	latestPod, err := findLatestJobPod(ctx, reader, job)
	if err == nil {
		probe, artifact, podMessage := buildStorageObservationFromPod(latestPod)
		if probe != nil {
			observation.StorageProbeResult = probe.Result
			observation.StorageProbeMessage = probe.Message
			if err := updateBackupStorageProbe(ctx, c, storage.Namespace, storage.Name, probe.Result, probe.Message); err != nil {
				return nil, err
			}
		}
		if phase == dpv1alpha1.ResourcePhaseFailed {
			observation.Message = fmt.Sprintf("%s; %s", message, podMessage)
		}
		if phase == dpv1alpha1.ResourcePhaseSucceeded {
			if artifact == nil {
				observation.Phase = dpv1alpha1.ResourcePhaseFailed
				observation.Message = "job completed but artifact summary is missing"
				return observation, nil
			}
			snapshotName, err := persistSnapshot(ctx, c, job, source, storage, policyRef, backupJobRef, series, artifact, keepLast)
			if err != nil {
				return nil, err
			}
			observation.SnapshotRef = snapshotName
			observation.Message = "job completed successfully"
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	if observation.StorageProbeResult == "" {
		if phase == dpv1alpha1.ResourcePhaseSucceeded {
			observation.StorageProbeResult = dpv1alpha1.ProbeResultSucceeded
		} else {
			observation.StorageProbeResult = dpv1alpha1.ProbeResultUnknown
		}
	}
	return observation, nil
}

func observeTerminalRestoreJob(ctx context.Context, c client.Client, reader client.Reader, job *batchv1.Job, storage *dpv1alpha1.BackupStorage) (*terminalRestoreObservation, error) {
	phase, message, done := jobTerminalState(job)
	if !done {
		return nil, nil
	}
	observation := &terminalRestoreObservation{
		Phase:       phase,
		Message:     message,
		CompletedAt: jobCompletedAt(job),
	}

	latestPod, err := findLatestJobPod(ctx, reader, job)
	if err == nil {
		probe, _, podMessage := buildStorageObservationFromPod(latestPod)
		if probe != nil {
			observation.StorageProbeResult = probe.Result
			observation.StorageProbeMessage = probe.Message
			if err := updateBackupStorageProbe(ctx, c, storage.Namespace, storage.Name, probe.Result, probe.Message); err != nil {
				return nil, err
			}
		}
		if phase == dpv1alpha1.ResourcePhaseFailed {
			observation.Message = fmt.Sprintf("%s; %s", message, podMessage)
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	if observation.StorageProbeResult == "" {
		if phase == dpv1alpha1.ResourcePhaseSucceeded {
			observation.StorageProbeResult = dpv1alpha1.ProbeResultSucceeded
		} else {
			observation.StorageProbeResult = dpv1alpha1.ProbeResultUnknown
		}
	}
	return observation, nil
}

func updateBackupStorageProbe(ctx context.Context, c client.Client, namespace, name string, result dpv1alpha1.ProbeResult, message string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var storage dpv1alpha1.BackupStorage
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &storage); err != nil {
			return err
		}
		storage.Status.ObservedGeneration = storage.Generation
		storage.Status.LastProbeTime = nowTime()
		storage.Status.LastProbeResult = result
		storage.Status.LastProbeMessage = trimString(message)
		if storage.Status.LastProbeMessage == "" {
			storage.Status.LastProbeMessage = phaseMessage(storage.Status.Phase)
		}
		return c.Status().Update(ctx, &storage)
	})
}

func persistSnapshot(ctx context.Context, c client.Client, job *batchv1.Job, source *dpv1alpha1.BackupSource, storage *dpv1alpha1.BackupStorage, policyRef, backupJobRef *corev1.LocalObjectReference, series string, artifact *artifactSummary, keepLast int32) (string, error) {
	snapshotName := dpv1alpha1.BuildSnapshotName(job.Name, storage.Name, "snapshot")
	snapshot := &dpv1alpha1.Snapshot{}
	key := types.NamespacedName{Namespace: job.Namespace, Name: snapshotName}
	err := c.Get(ctx, key, snapshot)
	if err != nil && !apierrors.IsNotFound(err) {
		return "", err
	}
	if apierrors.IsNotFound(err) {
		snapshot = &dpv1alpha1.Snapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snapshotName,
				Namespace: job.Namespace,
				Labels: map[string]string{
					managedByLabel:   managedByValue,
					operationLabel:   "snapshot",
					sourceNameLabel:  dpv1alpha1.BuildLabelValue(source.Name),
					storageNameLabel: dpv1alpha1.BuildLabelValue(storage.Name),
				},
				Annotations: map[string]string{
					seriesAnnotation:      series,
					backendPathAnnotation: artifact.BackendPath,
				},
			},
		}
	}
	snapshot.Spec.Series = series
	snapshot.Spec.SourceRef = corev1.LocalObjectReference{Name: source.Name}
	snapshot.Spec.StorageRef = corev1.LocalObjectReference{Name: storage.Name}
	snapshot.Spec.PolicyRef = policyRef
	snapshot.Spec.BackupJobRef = backupJobRef
	snapshot.Spec.NativeJobName = job.Name
	snapshot.Spec.BackendPath = artifact.BackendPath
	snapshot.Spec.Snapshot = artifact.Snapshot
	snapshot.Spec.Checksum = artifact.Checksum
	snapshot.Spec.Size = artifact.Size

	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, snapshot); err != nil {
			return "", err
		}
	} else {
		if err := c.Update(ctx, snapshot); err != nil {
			return "", err
		}
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var latest dpv1alpha1.Snapshot
		if err := c.Get(ctx, key, &latest); err != nil {
			return err
		}
		latest.Status.ObservedGeneration = latest.Generation
		latest.Status.Phase = dpv1alpha1.ResourcePhaseSucceeded
		latest.Status.StartedAt = job.Status.StartTime
		latest.Status.CompletedAt = jobCompletedAt(job)
		latest.Status.Message = "snapshot artifact is ready"
		latest.Status.ArtifactReady = true
		latest.Status.Latest = true
		markCondition(&latest.Status.Conditions, "Ready", metav1.ConditionTrue, "ArtifactReady", latest.Status.Message, latest.Generation)
		return c.Status().Update(ctx, &latest)
	}); err != nil {
		return "", err
	}

	if err := reconcileSnapshotSeries(ctx, c, job.Namespace, series, keepLast, snapshotName); err != nil {
		return "", err
	}
	return snapshotName, nil
}

func reconcileSnapshotSeries(ctx context.Context, c client.Client, namespace, series string, keepLast int32, latestSnapshotName string) error {
	var list dpv1alpha1.SnapshotList
	if err := c.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return err
	}
	items := make([]*dpv1alpha1.Snapshot, 0)
	for i := range list.Items {
		if list.Items[i].Spec.Series == series {
			items = append(items, &list.Items[i])
		}
	}
	sortSnapshotsNewestFirst(items)
	for index, snapshot := range items {
		if keepLast > 0 && int32(index) >= keepLast {
			if err := deleteInBackground(ctx, c, snapshot); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			continue
		}
		expectedLatest := snapshot.Name == latestSnapshotName
		if snapshot.Status.Latest == expectedLatest {
			continue
		}
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			var current dpv1alpha1.Snapshot
			if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: snapshot.Name}, &current); err != nil {
				return err
			}
			current.Status.Latest = expectedLatest
			return c.Status().Update(ctx, &current)
		}); err != nil {
			return err
		}
	}
	return nil
}

func trimJobFailureMessage(message string) string {
	parts := strings.Split(message, ";")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimString(part)
		if part != "" {
			clean = append(clean, part)
		}
	}
	sort.Strings(clean)
	return strings.Join(clean, "; ")
}
