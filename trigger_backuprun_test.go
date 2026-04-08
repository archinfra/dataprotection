package main

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func TestEnforceTriggeredRunConcurrencyForbidSkipsActiveRuns(t *testing.T) {
	scheme := newTriggerTestScheme(t)
	policy := newTriggerPolicy(batchv1.ForbidConcurrent)
	activeRun := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scheduled-001",
			Namespace: policy.Namespace,
			Labels: map[string]string{
				"dataprotection.archinfra.io/triggered-by": "cronjob",
				"dataprotection.archinfra.io/policy-name":  dpv1alpha1.BuildLabelValue(policy.Name),
				"dataprotection.archinfra.io/storage-name": dpv1alpha1.BuildLabelValue("minio-primary"),
			},
		},
		Status: dpv1alpha1.BackupRunStatus{Phase: dpv1alpha1.ResourcePhaseRunning},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, activeRun).Build()

	allowed, err := enforceTriggeredRunConcurrency(context.Background(), k8sClient, policy, "minio-primary")
	if err != nil {
		t.Fatalf("enforce concurrency: %v", err)
	}
	if allowed {
		t.Fatalf("expected forbid policy to skip when an active run already exists")
	}
}

func TestEnforceTriggeredRunConcurrencyReplaceDeletesActiveRuns(t *testing.T) {
	scheme := newTriggerTestScheme(t)
	policy := newTriggerPolicy(batchv1.ReplaceConcurrent)
	activeRun := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scheduled-001",
			Namespace: policy.Namespace,
			Labels: map[string]string{
				"dataprotection.archinfra.io/triggered-by": "cronjob",
				"dataprotection.archinfra.io/policy-name":  dpv1alpha1.BuildLabelValue(policy.Name),
				"dataprotection.archinfra.io/storage-name": dpv1alpha1.BuildLabelValue("minio-primary"),
			},
		},
		Status: dpv1alpha1.BackupRunStatus{Phase: dpv1alpha1.ResourcePhaseRunning},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, activeRun).Build()

	allowed, err := enforceTriggeredRunConcurrency(context.Background(), k8sClient, policy, "minio-primary")
	if err != nil {
		t.Fatalf("enforce concurrency: %v", err)
	}
	if !allowed {
		t.Fatalf("expected replace policy to allow a new run after deleting active ones")
	}

	var lookup dpv1alpha1.BackupRun
	err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: policy.Namespace, Name: activeRun.Name}, &lookup)
	if err == nil {
		t.Fatalf("expected active run to be deleted by replace concurrency")
	}
}

func newTriggerPolicy(concurrencyPolicy batchv1.ConcurrencyPolicy) *dpv1alpha1.BackupPolicy {
	return &dpv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-smoke-daily", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupPolicySpec{
			SourceRef:   corev1.LocalObjectReference{Name: "mysql-smoke"},
			StorageRefs: []corev1.LocalObjectReference{{Name: "minio-primary"}},
			Schedule: dpv1alpha1.BackupScheduleSpec{
				Cron:              "*/3 * * * *",
				ConcurrencyPolicy: concurrencyPolicy,
			},
		},
	}
}

func newTriggerTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := dpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add data protection scheme: %v", err)
	}
	return scheme
}
