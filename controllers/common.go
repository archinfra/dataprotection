package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func nowTime() *metav1.Time {
	t := metav1.NewTime(time.Now().UTC())
	return &t
}

func markCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	})
}

func phaseMessage(phase dpv1alpha1.ResourcePhase) string {
	switch phase {
	case dpv1alpha1.ResourcePhaseReady:
		return "resource is ready"
	case dpv1alpha1.ResourcePhaseRunning:
		return "resource is actively running"
	case dpv1alpha1.ResourcePhaseSucceeded:
		return "request completed successfully"
	case dpv1alpha1.ResourcePhaseFailed:
		return "resource reconciliation failed"
	case dpv1alpha1.ResourcePhasePaused:
		return "resource is suspended"
	default:
		return "resource is pending reconciliation"
	}
}

func requeueSoon() ctrl.Result {
	return ctrl.Result{RequeueAfter: 30 * time.Second}
}

func resolveKeepLastRetention(ctx context.Context, c client.Client, namespace string, inline dpv1alpha1.RetentionRule, refName string) (int32, *dpv1alpha1.RetentionPolicy, error) {
	keepLast := retentionValue(inline)
	refName = trimString(refName)
	if refName == "" {
		return keepLast, nil, nil
	}

	retentionPolicy, err := getRetentionPolicy(ctx, c, namespace, refName)
	if err != nil {
		return 0, nil, err
	}
	if err := retentionPolicy.Spec.ValidateBasic(); err != nil {
		return 0, retentionPolicy, newPermanentDependencyError("referenced RetentionPolicy %q is invalid: %v", retentionPolicy.Name, err)
	}
	if retentionPolicy.Spec.SuccessfulSnapshots.Last > 0 {
		keepLast = retentionPolicy.Spec.SuccessfulSnapshots.Last
	}
	return keepLast, retentionPolicy, nil
}

func resolveFailedRetention(ctx context.Context, c client.Client, namespace, refName string) (int32, *dpv1alpha1.RetentionPolicy, error) {
	refName = trimString(refName)
	if refName == "" {
		return 0, nil, nil
	}

	retentionPolicy, err := getRetentionPolicy(ctx, c, namespace, refName)
	if err != nil {
		return 0, nil, err
	}
	if err := retentionPolicy.Spec.ValidateBasic(); err != nil {
		return 0, retentionPolicy, newPermanentDependencyError("referenced RetentionPolicy %q is invalid: %v", retentionPolicy.Name, err)
	}
	return retentionPolicy.Spec.FailedSnapshots.Last, retentionPolicy, nil
}

func trimString(value string) string {
	return strings.TrimSpace(value)
}

func localRefName(ref *corev1.LocalObjectReference) string {
	if ref == nil {
		return ""
	}
	return trimString(ref.Name)
}

func snapshotSortTime(snapshot *dpv1alpha1.Snapshot) time.Time {
	if snapshot.Status.CompletedAt != nil {
		return snapshot.Status.CompletedAt.Time
	}
	if snapshot.Status.StartedAt != nil {
		return snapshot.Status.StartedAt.Time
	}
	return snapshot.CreationTimestamp.Time
}

func sortSnapshotsNewestFirst(items []*dpv1alpha1.Snapshot) {
	sort.SliceStable(items, func(i, j int) bool {
		left := snapshotSortTime(items[i])
		right := snapshotSortTime(items[j])
		if left.Equal(right) {
			return items[i].Name > items[j].Name
		}
		return left.After(right)
	})
}

func backupRunSortTime(run *dpv1alpha1.BackupRun) time.Time {
	if run.Status.CompletedAt != nil {
		return run.Status.CompletedAt.Time
	}
	if run.Status.StartedAt != nil {
		return run.Status.StartedAt.Time
	}
	return run.CreationTimestamp.Time
}

func sortBackupRunsNewestFirst(items []*dpv1alpha1.BackupRun) {
	sort.SliceStable(items, func(i, j int) bool {
		left := backupRunSortTime(items[i])
		right := backupRunSortTime(items[j])
		if left.Equal(right) {
			return items[i].Name > items[j].Name
		}
		return left.After(right)
	})
}

func createOrUpdateWithRetry(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	mutateFn controllerutil.MutateFn,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, c, obj, mutateFn)
		return err
	})
}

func isTerminalBackupRun(phase dpv1alpha1.ResourcePhase) bool {
	switch phase {
	case dpv1alpha1.ResourcePhaseSucceeded, dpv1alpha1.ResourcePhaseFailed:
		return true
	default:
		return false
	}
}

func describeLatestJobPodFailure(ctx context.Context, c client.Client, job *batchv1.Job) string {
	var podList corev1.PodList
	if err := c.List(ctx, &podList,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{"job-name": job.Name},
	); err != nil {
		return ""
	}
	if len(podList.Items) == 0 {
		return ""
	}

	sort.SliceStable(podList.Items, func(i, j int) bool {
		left := podList.Items[i].CreationTimestamp.Time
		right := podList.Items[j].CreationTimestamp.Time
		if left.Equal(right) {
			return podList.Items[i].Name > podList.Items[j].Name
		}
		return left.After(right)
	})
	latest := &podList.Items[0]

	failures := collectContainerFailures(latest.Status.InitContainerStatuses, true)
	failures = append(failures, collectContainerFailures(latest.Status.ContainerStatuses, false)...)
	if len(failures) == 0 {
		return fmt.Sprintf("latest pod %s phase=%s", latest.Name, latest.Status.Phase)
	}
	return fmt.Sprintf("latest pod %s: %s", latest.Name, strings.Join(failures, "; "))
}

func collectContainerFailures(statuses []corev1.ContainerStatus, init bool) []string {
	failures := make([]string, 0)
	prefix := "container"
	if init {
		prefix = "init"
	}
	for _, status := range statuses {
		if terminated := status.State.Terminated; terminated != nil {
			if terminated.ExitCode == 0 {
				continue
			}
			reason := strings.TrimSpace(terminated.Reason)
			if reason == "" {
				reason = "terminated"
			}
			failures = append(failures, fmt.Sprintf("%s %s exited %d (%s)", prefix, status.Name, terminated.ExitCode, reason))
			continue
		}
		if waiting := status.State.Waiting; waiting != nil {
			reason := strings.TrimSpace(waiting.Reason)
			if reason == "" {
				reason = "waiting"
			}
			failures = append(failures, fmt.Sprintf("%s %s waiting (%s)", prefix, status.Name, reason))
		}
	}
	return failures
}
