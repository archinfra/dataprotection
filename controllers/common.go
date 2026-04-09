package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	case dpv1alpha1.ResourcePhaseConfigured:
		return "resource specification is configured"
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

func trimString(value string) string {
	return strings.TrimSpace(value)
}

func localRefName(ref *corev1.LocalObjectReference) string {
	if ref == nil {
		return ""
	}
	return trimString(ref.Name)
}

func createOrUpdateWithRetry(ctx context.Context, c client.Client, obj client.Object, mutateFn controllerutil.MutateFn) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, c, obj, mutateFn)
		return err
	})
}

func deleteInBackground(ctx context.Context, c client.Client, obj client.Object) error {
	policy := metav1.DeletePropagationBackground
	return c.Delete(ctx, obj, client.PropagationPolicy(policy))
}

func getBackupAddon(ctx context.Context, c client.Client, name string) (*dpv1alpha1.BackupAddon, error) {
	var addon dpv1alpha1.BackupAddon
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &addon); err != nil {
		return nil, err
	}
	return &addon, nil
}

func getBackupSource(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupSource, error) {
	var source dpv1alpha1.BackupSource
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &source); err != nil {
		return nil, err
	}
	return &source, nil
}

func getBackupStorage(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupStorage, error) {
	var storage dpv1alpha1.BackupStorage
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &storage); err != nil {
		return nil, err
	}
	return &storage, nil
}

func getBackupPolicy(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupPolicy, error) {
	var policy dpv1alpha1.BackupPolicy
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func getRetentionPolicy(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.RetentionPolicy, error) {
	var retention dpv1alpha1.RetentionPolicy
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &retention); err != nil {
		return nil, err
	}
	return &retention, nil
}

func getSnapshot(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.Snapshot, error) {
	var snapshot dpv1alpha1.Snapshot
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func getNotificationEndpoint(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.NotificationEndpoint, error) {
	var endpoint dpv1alpha1.NotificationEndpoint
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &endpoint); err != nil {
		return nil, err
	}
	return &endpoint, nil
}

func effectiveSuccessfulKeepLast(policy *dpv1alpha1.RetentionPolicy) int32 {
	value := int32(3)
	if policy == nil {
		return value
	}
	if policy.Spec.SuccessfulSnapshots.KeepLast > 0 {
		return policy.Spec.SuccessfulSnapshots.KeepLast
	}
	return value
}

func effectiveFailedKeepLast(policy *dpv1alpha1.RetentionPolicy) int32 {
	value := int32(1)
	if policy == nil {
		return value
	}
	if policy.Spec.FailedExecutions.KeepLast > 0 {
		return policy.Spec.FailedExecutions.KeepLast
	}
	return value
}

func hasOwnerRef(obj metav1.Object, kind, name string) bool {
	for _, owner := range obj.GetOwnerReferences() {
		if owner.Kind == kind && owner.Name == name {
			return true
		}
	}
	return false
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

func snapshotSortTime(snapshot *dpv1alpha1.Snapshot) time.Time {
	if snapshot.Status.CompletedAt != nil {
		return snapshot.Status.CompletedAt.Time
	}
	if snapshot.Status.StartedAt != nil {
		return snapshot.Status.StartedAt.Time
	}
	return snapshot.CreationTimestamp.Time
}

func notificationRefNames(refs []corev1.LocalObjectReference) string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		if name := trimString(ref.Name); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

type podExecutionSummary struct {
	StorageProbe *storageProbeSummary `json:"storageProbe,omitempty"`
	Artifact     *artifactSummary     `json:"artifact,omitempty"`
	Message      string               `json:"message,omitempty"`
}

type storageProbeSummary struct {
	Result  dpv1alpha1.ProbeResult `json:"result,omitempty"`
	Message string                 `json:"message,omitempty"`
}

type artifactSummary struct {
	Snapshot    string `json:"snapshot,omitempty"`
	BackendPath string `json:"backendPath,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
	Size        int64  `json:"size,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

func parseJSONTerminationMessage(message string) (*podExecutionSummary, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, nil
	}
	var summary podExecutionSummary
	if err := json.Unmarshal([]byte(message), &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func findLatestJobPod(ctx context.Context, reader client.Reader, job *batchv1.Job) (*corev1.Pod, error) {
	var podList corev1.PodList
	if err := reader.List(ctx, &podList,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{"job-name": job.Name},
	); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, apierrors.NewNotFound(corev1.Resource("pods"), job.Name)
	}
	sort.SliceStable(podList.Items, func(i, j int) bool {
		left := podList.Items[i].CreationTimestamp.Time
		right := podList.Items[j].CreationTimestamp.Time
		if left.Equal(right) {
			return podList.Items[i].Name > podList.Items[j].Name
		}
		return left.After(right)
	})
	return &podList.Items[0], nil
}

func latestContainerFailureMessage(pod *corev1.Pod) string {
	failures := collectContainerFailures(pod.Status.InitContainerStatuses, true)
	failures = append(failures, collectContainerFailures(pod.Status.ContainerStatuses, false)...)
	if len(failures) == 0 {
		return fmt.Sprintf("latest pod %s phase=%s", pod.Name, pod.Status.Phase)
	}
	return fmt.Sprintf("latest pod %s: %s", pod.Name, strings.Join(failures, "; "))
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
