package controllers

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

const (
	adoptedNativeJobAnnotation     = "dataprotection.archinfra.io/adopted-native-job"
	backupExecutionNameAnnotation = "dataprotection.archinfra.io/backup-execution-name"
)

func resolveBackupExecutionDependencies(ctx context.Context, c client.Client, execution *dpv1alpha1.BackupExecution) (*resolvedBackupExecution, error) {
	source, err := getBackupSource(ctx, c, execution.Namespace, execution.Spec.SourceRef.Name)
	if err != nil {
		return nil, err
	}
	addon, err := getBackupAddon(ctx, c, source.Spec.AddonRef.Name)
	if err != nil {
		return nil, err
	}
	storage, err := getBackupStorage(ctx, c, execution.Namespace, execution.Spec.StorageRef.Name)
	if err != nil {
		return nil, err
	}

	var policy *dpv1alpha1.BackupPolicy
	if execution.Spec.PolicyRef != nil && trimString(execution.Spec.PolicyRef.Name) != "" {
		policy, err = getBackupPolicy(ctx, c, execution.Namespace, execution.Spec.PolicyRef.Name)
		if err != nil {
			return nil, err
		}
	}

	retentionRef := localRefName(execution.Spec.RetentionRef)
	if retentionRef == "" && policy != nil {
		retentionRef = localRefName(policy.Spec.RetentionRef)
	}
	retention, err := resolveRetentionPolicy(ctx, c, execution.Namespace, retentionRef)
	if err != nil {
		return nil, err
	}

	policyName := localRefName(execution.Spec.PolicyRef)
	series := buildSeries(source, storage.Name, policyName, execution.Name)
	return &resolvedBackupExecution{
		Policy:          policy,
		Source:          source,
		Addon:           addon,
		Storage:         storage,
		RetentionPolicy: retention,
		Series:          series,
		BackendPath:     buildBackendPath(source, storage.Name, policyName, execution.Name),
		KeepLast:        effectiveSuccessfulKeepLast(retention),
	}, nil
}

func buildBackupExecutionNativeJob(execution *dpv1alpha1.BackupExecution, resolved *resolvedBackupExecution) (*batchv1.Job, error) {
	spec, labels, annotations, err := buildBackupJobSpec(
		"BackupExecution",
		execution.Name,
		resolved.Series,
		resolved.BackendPath,
		mergeJobRuntime(execution.Spec.JobRuntime, resolved.Policy),
		resolveJobNotificationRefs(execution.Spec.NotificationRefs, resolved.Policy),
		resolved.Source,
		resolved.Addon,
		resolved.Storage,
		resolved.KeepLast,
		execution.Spec.SnapshotName,
	)
	if err != nil {
		return nil, err
	}
	if resolved.Policy != nil {
		labels[policyNameLabel] = dpv1alpha1.BuildLabelValue(resolved.Policy.Name)
	}
	annotations[backupExecutionNameAnnotation] = execution.Name
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpv1alpha1.BuildJobName(execution.Name, "backup"),
			Namespace:   execution.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
	}, nil
}

func observeTerminalBackupExecution(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	nativeJob *batchv1.Job,
	execution *dpv1alpha1.BackupExecution,
	resolved *resolvedBackupExecution,
) (*terminalBackupObservation, error) {
	observation, err := observeTerminalBackupJob(
		ctx,
		c,
		reader,
		nativeJob,
		resolved.Source,
		resolved.Storage,
		func() *corev1.LocalObjectReference {
			if resolved.Policy == nil {
				return nil
			}
			return &corev1.LocalObjectReference{Name: resolved.Policy.Name}
		}(),
		nil,
		resolved.Series,
		resolved.KeepLast,
	)
	if err != nil || observation == nil || observation.SnapshotRef == "" {
		return observation, err
	}
	if err := annotateSnapshotWithBackupExecution(ctx, c, execution.Namespace, observation.SnapshotRef, execution.Name); err != nil {
		return nil, err
	}
	return observation, nil
}

func annotateSnapshotWithBackupExecution(ctx context.Context, c client.Client, namespace, snapshotName, executionName string) error {
	var snapshot dpv1alpha1.Snapshot
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: snapshotName}, &snapshot); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if snapshot.Annotations != nil && snapshot.Annotations[backupExecutionNameAnnotation] == executionName {
		return nil
	}
	base := snapshot.DeepCopy()
	if snapshot.Annotations == nil {
		snapshot.Annotations = map[string]string{}
	}
	snapshot.Annotations[backupExecutionNameAnnotation] = executionName
	return c.Patch(ctx, &snapshot, client.MergeFrom(base))
}
