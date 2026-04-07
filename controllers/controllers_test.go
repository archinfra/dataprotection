package controllers

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func TestBackupPolicyReconcileCreatesCronJobsAndCleansUpStale(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("source-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
	repositoryA := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-a", Namespace: "backup-system", UID: types.UID("repo-a")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeNFS,
			NFS:  &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.10", Path: "/data/a"},
		},
	}
	repositoryB := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-b", Namespace: "backup-system", UID: types.UID("repo-b")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeNFS,
			NFS:  &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.11", Path: "/data/b"},
		},
	}
	policy := &dpv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-daily", Namespace: "backup-system", UID: types.UID("policy-uid")},
		Spec: dpv1alpha1.BackupPolicySpec{
			SourceRef: corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs: []corev1.LocalObjectReference{
				{Name: repositoryA.Name},
				{Name: repositoryB.Name},
			},
			Schedule: dpv1alpha1.BackupScheduleSpec{Cron: "0 2 * * *"},
		},
	}
	stale := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpv1alpha1.BuildCronJobName(policy.Name, "stale-repo"),
			Namespace: policy.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(policy, stale, scheme); err != nil {
		t.Fatalf("set stale cronjob owner reference: %v", err)
	}

	fakeClient := newFakeClient(t, scheme, source, repositoryA, repositoryB, policy, stale)
	reconciler := &BackupPolicyReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(policy)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	expectedNames := []string{
		dpv1alpha1.BuildCronJobName(policy.Name, repositoryA.Name),
		dpv1alpha1.BuildCronJobName(policy.Name, repositoryB.Name),
	}
	for _, name := range expectedNames {
		var cronJob batchv1.CronJob
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: name}, &cronJob); err != nil {
			t.Fatalf("expected cronjob %s: %v", name, err)
		}
	}
	var removed batchv1.CronJob
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: stale.Name}, &removed); err == nil {
		t.Fatalf("stale cronjob %s should have been deleted", stale.Name)
	}

	var cronJobs batchv1.CronJobList
	if err := fakeClient.List(ctx, &cronJobs, client.InNamespace(policy.Namespace)); err != nil {
		t.Fatalf("list cronjobs: %v", err)
	}
	if len(cronJobs.Items) != 2 {
		t.Fatalf("expected 2 cronjobs, got %d", len(cronJobs.Items))
	}

	var updatedPolicy dpv1alpha1.BackupPolicy
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(policy), &updatedPolicy); err != nil {
		t.Fatalf("get updated policy: %v", err)
	}
	if updatedPolicy.Status.Phase != dpv1alpha1.ResourcePhaseReady {
		t.Fatalf("expected Ready phase, got %s", updatedPolicy.Status.Phase)
	}
	if len(updatedPolicy.Status.CronJobNames) != 2 {
		t.Fatalf("expected 2 cronjob names in status, got %d", len(updatedPolicy.Status.CronJobNames))
	}
}

func TestBackupPolicyReconcileAcceptsRetentionPolicyRef(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("source-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-a", Namespace: "backup-system", UID: types.UID("repo-a")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeNFS,
			NFS:  &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.10", Path: "/data/a"},
		},
	}
	retentionPolicy := &dpv1alpha1.RetentionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "daily-retention", Namespace: "backup-system", UID: types.UID("retention-uid")},
		Spec: dpv1alpha1.RetentionPolicySpec{
			SuccessfulSnapshots: dpv1alpha1.SnapshotRetentionRule{Last: 7},
		},
	}
	policy := &dpv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-daily", Namespace: "backup-system", UID: types.UID("policy-uid")},
		Spec: dpv1alpha1.BackupPolicySpec{
			SourceRef:          corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs:     []corev1.LocalObjectReference{{Name: repository.Name}},
			Schedule:           dpv1alpha1.BackupScheduleSpec{Cron: "0 2 * * *"},
			RetentionPolicyRef: &corev1.LocalObjectReference{Name: retentionPolicy.Name},
		},
	}

	fakeClient := newFakeClient(t, scheme, source, repository, retentionPolicy, policy)
	reconciler := &BackupPolicyReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(policy)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updatedPolicy dpv1alpha1.BackupPolicy
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(policy), &updatedPolicy); err != nil {
		t.Fatalf("get updated policy: %v", err)
	}
	if updatedPolicy.Status.Phase != dpv1alpha1.ResourcePhaseReady {
		t.Fatalf("expected Ready phase, got %s", updatedPolicy.Status.Phase)
	}
}

func TestResolveRepositoriesHydratesStorageRefAndPath(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	storage := &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "minio-primary", Namespace: "backup-system", UID: types.UID("storage-uid")},
		Spec: dpv1alpha1.BackupStorageSpec{
			Type: dpv1alpha1.RepositoryTypeS3,
			S3: &dpv1alpha1.S3RepositorySpec{
				Endpoint: "https://minio.example.com",
				Bucket:   "data-protection",
				Prefix:   "platform",
			},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("repo-uid")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			StorageRef: &corev1.LocalObjectReference{Name: storage.Name},
			Path:       "mysql/prod",
		},
	}

	fakeClient := newFakeClient(t, scheme, storage, repository)
	repositories, err := resolveRepositories(ctx, fakeClient, repository.Namespace, []corev1.LocalObjectReference{{Name: repository.Name}})
	if err != nil {
		t.Fatalf("resolve repositories: %v", err)
	}
	if len(repositories) != 1 {
		t.Fatalf("expected 1 resolved repository, got %d", len(repositories))
	}
	if repositories[0].Spec.Type != dpv1alpha1.RepositoryTypeS3 {
		t.Fatalf("expected resolved repository type s3, got %s", repositories[0].Spec.Type)
	}
	if repositories[0].Spec.S3 == nil {
		t.Fatalf("expected resolved repository s3 config")
	}
	if got := repositories[0].Spec.S3.Prefix; got != "platform/mysql/prod" {
		t.Fatalf("expected resolved s3 prefix platform/mysql/prod, got %q", got)
	}
}

func TestResolveRepositoriesUsesDefaultBackupStorage(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	storage := &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-default", Namespace: "backup-system", UID: types.UID("storage-uid")},
		Spec: dpv1alpha1.BackupStorageSpec{
			Default: true,
			Type:    dpv1alpha1.RepositoryTypeNFS,
			NFS:     &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.10", Path: "/exports/backups"},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("repo-uid")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Path: "mysql/prod",
		},
	}

	fakeClient := newFakeClient(t, scheme, storage, repository)
	repositories, err := resolveRepositories(ctx, fakeClient, repository.Namespace, []corev1.LocalObjectReference{{Name: repository.Name}})
	if err != nil {
		t.Fatalf("resolve repositories: %v", err)
	}
	if len(repositories) != 1 {
		t.Fatalf("expected 1 resolved repository, got %d", len(repositories))
	}
	if repositories[0].Spec.NFS == nil {
		t.Fatalf("expected resolved repository nfs config")
	}
	if got := repositories[0].Spec.NFS.Path; got != "/exports/backups/mysql/prod" {
		t.Fatalf("expected resolved nfs path /exports/backups/mysql/prod, got %q", got)
	}
}

func TestResolveRestoreRepositoryHydratesStorageRef(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	storage := &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "minio-primary", Namespace: "backup-system", UID: types.UID("storage-uid")},
		Spec: dpv1alpha1.BackupStorageSpec{
			Type: dpv1alpha1.RepositoryTypeS3,
			S3: &dpv1alpha1.S3RepositorySpec{
				Endpoint: "https://minio.example.com",
				Bucket:   "data-protection",
				Prefix:   "platform",
			},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("repo-uid")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			StorageRef: &corev1.LocalObjectReference{Name: storage.Name},
			Path:       "mysql/prod",
		},
	}
	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-source", Namespace: "backup-system", UID: types.UID("source-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
	backupRun := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef:      corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs: []corev1.LocalObjectReference{{Name: repository.Name}},
			Snapshot:       "snapshot-001",
		},
		Status: dpv1alpha1.BackupRunStatus{
			Repositories: []dpv1alpha1.RepositoryRunStatus{
				{Name: repository.Name, Snapshot: "snapshot-001", Phase: dpv1alpha1.ResourcePhaseSucceeded},
			},
		},
	}
	restore := &dpv1alpha1.RestoreRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-001", Namespace: "backup-system", UID: types.UID("restore-uid")},
		Spec: dpv1alpha1.RestoreRequestSpec{
			SourceRef:     corev1.LocalObjectReference{Name: source.Name},
			BackupRunRef:  &corev1.LocalObjectReference{Name: backupRun.Name},
			RepositoryRef: &corev1.LocalObjectReference{Name: repository.Name},
			Target:        dpv1alpha1.RestoreTargetSpec{Mode: dpv1alpha1.RestoreTargetModeInPlace},
		},
	}

	fakeClient := newFakeClient(t, scheme, storage, repository, source, backupRun, restore)
	_, _, resolvedRepository, err := resolveRestoreRepository(ctx, fakeClient, restore)
	if err != nil {
		t.Fatalf("resolve restore repository: %v", err)
	}
	if resolvedRepository.Spec.S3 == nil {
		t.Fatalf("expected resolved s3 repository")
	}
	if got := resolvedRepository.Spec.S3.Prefix; got != "platform/mysql/prod" {
		t.Fatalf("expected resolved restore s3 prefix platform/mysql/prod, got %q", got)
	}
}

func TestBackupRunReconcileCreatesJobsAndTracksCompletion(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("source-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
	repositoryA := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-a", Namespace: "backup-system", UID: types.UID("repo-a")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeNFS,
			NFS:  &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.10", Path: "/data/a"},
		},
	}
	repositoryB := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "minio-b", Namespace: "backup-system", UID: types.UID("repo-b")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeS3,
			S3:   &dpv1alpha1.S3RepositorySpec{Endpoint: "https://minio.example.com", Bucket: "backup"},
		},
	}
	run := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef: corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs: []corev1.LocalObjectReference{
				{Name: repositoryA.Name},
				{Name: repositoryB.Name},
			},
			Reason: "smoke test",
		},
	}

	fakeClient := newFakeClient(t, scheme, source, repositoryA, repositoryB, run)
	reconciler := &BackupRunReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	jobNames := []string{
		dpv1alpha1.BuildJobName(run.Name, repositoryA.Name),
		dpv1alpha1.BuildJobName(run.Name, repositoryB.Name),
	}
	for _, name := range jobNames {
		var job batchv1.Job
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: name}, &job); err != nil {
			t.Fatalf("expected job %s: %v", name, err)
		}
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{
				Type:               batchv1.JobComplete,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
			},
		}
		if err := fakeClient.Status().Update(ctx, &job); err != nil {
			t.Fatalf("update job status %s: %v", name, err)
		}
	}

	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	var updatedRun dpv1alpha1.BackupRun
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(run), &updatedRun); err != nil {
		t.Fatalf("get updated run: %v", err)
	}
	if updatedRun.Status.Phase != dpv1alpha1.ResourcePhaseSucceeded {
		t.Fatalf("expected Succeeded phase, got %s", updatedRun.Status.Phase)
	}
	if len(updatedRun.Status.JobNames) != 2 {
		t.Fatalf("expected 2 job names in status, got %d", len(updatedRun.Status.JobNames))
	}
	if len(updatedRun.Status.Repositories) != 2 {
		t.Fatalf("expected 2 repository statuses, got %d", len(updatedRun.Status.Repositories))
	}
	if updatedRun.Status.CompletedAt == nil {
		t.Fatalf("expected completedAt to be set")
	}
	for _, repository := range []string{repositoryA.Name, repositoryB.Name} {
		snapshotName := dpv1alpha1.BuildSnapshotName(run.Name, repository, "snapshot")
		var snapshot dpv1alpha1.Snapshot
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: snapshotName}, &snapshot); err != nil {
			t.Fatalf("expected snapshot %s: %v", snapshotName, err)
		}
		if snapshot.Spec.BackupRunRef.Name != run.Name {
			t.Fatalf("expected snapshot %s to reference run %s, got %s", snapshotName, run.Name, snapshot.Spec.BackupRunRef.Name)
		}
	}
}

func TestRestoreRequestReconcileCreatesSingleJobFromBackupRun(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("source-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-a", Namespace: "backup-system", UID: types.UID("repo-a")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeNFS,
			NFS:  &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.10", Path: "/data/a"},
		},
	}
	backupRun := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef:      corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs: []corev1.LocalObjectReference{{Name: repository.Name}},
			Snapshot:       "snapshot-001",
		},
		Status: dpv1alpha1.BackupRunStatus{
			Repositories: []dpv1alpha1.RepositoryRunStatus{
				{Name: repository.Name, Snapshot: "snapshot-001", Phase: dpv1alpha1.ResourcePhaseSucceeded},
			},
		},
	}
	restore := &dpv1alpha1.RestoreRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-001", Namespace: "backup-system", UID: types.UID("restore-uid")},
		Spec: dpv1alpha1.RestoreRequestSpec{
			SourceRef:    corev1.LocalObjectReference{Name: source.Name},
			BackupRunRef: &corev1.LocalObjectReference{Name: backupRun.Name},
			Target:       dpv1alpha1.RestoreTargetSpec{Mode: dpv1alpha1.RestoreTargetModeInPlace},
		},
	}

	fakeClient := newFakeClient(t, scheme, source, repository, backupRun, restore)
	reconciler := &RestoreRequestReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(restore)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	jobName := dpv1alpha1.BuildJobName(restore.Name, "restore")
	var job batchv1.Job
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: jobName}, &job); err != nil {
		t.Fatalf("expected restore job %s: %v", jobName, err)
	}
	job.Status.Succeeded = 1
	job.Status.Conditions = []batchv1.JobCondition{
		{
			Type:               batchv1.JobComplete,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
	if err := fakeClient.Status().Update(ctx, &job); err != nil {
		t.Fatalf("update restore job status: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	var updatedRestore dpv1alpha1.RestoreRequest
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(restore), &updatedRestore); err != nil {
		t.Fatalf("get updated restore request: %v", err)
	}
	if updatedRestore.Status.Phase != dpv1alpha1.ResourcePhaseSucceeded {
		t.Fatalf("expected Succeeded phase, got %s", updatedRestore.Status.Phase)
	}
	if updatedRestore.Status.JobName != jobName {
		t.Fatalf("expected job name %s, got %s", jobName, updatedRestore.Status.JobName)
	}
	if updatedRestore.Status.CompletedAt == nil {
		t.Fatalf("expected completedAt to be set")
	}
}

func TestRestoreRequestReconcileCreatesSingleJobFromSnapshotRef(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system", UID: types.UID("source-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-a", Namespace: "backup-system", UID: types.UID("repo-a")},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeNFS,
			NFS:  &dpv1alpha1.NFSRepositorySpec{Server: "10.0.0.10", Path: "/data/a"},
		},
	}
	backupRun := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef:      corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs: []corev1.LocalObjectReference{{Name: repository.Name}},
			Snapshot:       "snapshot-001",
		},
	}
	snapshot := &dpv1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: dpv1alpha1.BuildSnapshotName(backupRun.Name, repository.Name, "snapshot"), Namespace: "backup-system", UID: types.UID("snapshot-uid")},
		Spec: dpv1alpha1.SnapshotSpec{
			SourceRef:     corev1.LocalObjectReference{Name: source.Name},
			BackupRunRef:  corev1.LocalObjectReference{Name: backupRun.Name},
			RepositoryRef: corev1.LocalObjectReference{Name: repository.Name},
			Driver:        dpv1alpha1.BackupDriverMySQL,
			Snapshot:      "snapshot-001",
		},
		Status: dpv1alpha1.SnapshotStatus{
			Phase: dpv1alpha1.ResourcePhaseSucceeded,
		},
	}
	restore := &dpv1alpha1.RestoreRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-by-snapshot", Namespace: "backup-system", UID: types.UID("restore-uid")},
		Spec: dpv1alpha1.RestoreRequestSpec{
			SourceRef:   corev1.LocalObjectReference{Name: source.Name},
			SnapshotRef: &corev1.LocalObjectReference{Name: snapshot.Name},
			Target:      dpv1alpha1.RestoreTargetSpec{Mode: dpv1alpha1.RestoreTargetModeInPlace},
		},
	}

	fakeClient := newFakeClient(t, scheme, source, repository, backupRun, snapshot, restore)
	reconciler := &RestoreRequestReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(restore)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	jobName := dpv1alpha1.BuildJobName(restore.Name, "restore")
	var job batchv1.Job
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: jobName}, &job); err != nil {
		t.Fatalf("expected restore job %s: %v", jobName, err)
	}
	if got := job.Annotations[snapshotAnnotation]; got != "snapshot-001.sql.gz" {
		t.Fatalf("expected snapshot annotation snapshot-001.sql.gz, got %q", got)
	}
}

func TestBuildBuiltInMySQLBackupRunJobUsesRepositorySpecificRuntime(t *testing.T) {
	source := &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-prod", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host:     "mysql.default.svc",
				Port:     3306,
				Username: "root",
			},
		},
	}
	repository := &dpv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "minio-b", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupRepositorySpec{
			Type: dpv1alpha1.RepositoryTypeS3,
			S3:   &dpv1alpha1.S3RepositorySpec{Endpoint: "https://minio.example.com", Bucket: "backup"},
		},
	}
	run := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef:      corev1.LocalObjectReference{Name: source.Name},
			RepositoryRefs: []corev1.LocalObjectReference{{Name: repository.Name}},
			Snapshot:       "snapshot-001",
		},
	}

	job, err := buildBackupRunJob(run, nil, source, repository, "snapshot-001", 5)
	if err != nil {
		t.Fatalf("build backup run job: %v", err)
	}
	if len(job.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("expected 1 init container for s3 prefetch, got %d", len(job.Spec.Template.Spec.InitContainers))
	}
	if len(job.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers for mysql+s3 upload, got %d", len(job.Spec.Template.Spec.Containers))
	}
	if job.Spec.Template.Spec.Containers[0].Name != "mysql-backup" {
		t.Fatalf("expected first container to be mysql-backup, got %s", job.Spec.Template.Spec.Containers[0].Name)
	}
	if job.Spec.Template.Spec.Containers[1].Name != "s3-upload" {
		t.Fatalf("expected second container to be s3-upload, got %s", job.Spec.Template.Spec.Containers[1].Name)
	}
	if len(job.Spec.Template.Spec.Volumes) != 2 {
		t.Fatalf("expected 2 volumes for s3 staging/status, got %d", len(job.Spec.Template.Spec.Volumes))
	}
	if got := job.Annotations[snapshotAnnotation]; got != "snapshot-001.sql.gz" {
		t.Fatalf("expected snapshot annotation snapshot-001.sql.gz, got %q", got)
	}
}

func newTestScheme(t *testing.T) *runtime.Scheme {
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

func newFakeClient(t *testing.T, scheme *runtime.Scheme, objects ...client.Object) client.Client {
	t.Helper()

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(
			&dpv1alpha1.BackupStorage{},
			&dpv1alpha1.BackupSource{},
			&dpv1alpha1.BackupRepository{},
			&dpv1alpha1.BackupPolicy{},
			&dpv1alpha1.BackupRun{},
			&dpv1alpha1.RestoreRequest{},
			&dpv1alpha1.Snapshot{},
			&dpv1alpha1.RetentionPolicy{},
			&batchv1.Job{},
			&batchv1.CronJob{},
		).
		WithObjects(objects...).
		Build()
}
