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

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func TestBackupPolicyReconcileCreatesCronJobsPerStorage(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := newMySQLSource("backup-system", "mysql-prod")
	storageA := newNFSStorage("backup-system", "nfs-a", "/exports/a")
	storageB := newS3Storage("backup-system", "minio-b", "platform")
	policy := &dpv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-daily", Namespace: "backup-system", UID: types.UID("policy-uid")},
		Spec: dpv1alpha1.BackupPolicySpec{
			SourceRef: corev1.LocalObjectReference{Name: source.Name},
			StorageRefs: []corev1.LocalObjectReference{
				{Name: storageA.Name},
				{Name: storageB.Name},
			},
			Schedule: dpv1alpha1.BackupScheduleSpec{Cron: "0 2 * * *"},
		},
	}

	fakeClient := newFakeClient(t, scheme, source, storageA, storageB, policy)
	reconciler := &BackupPolicyReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(policy)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	for _, storageName := range []string{storageA.Name, storageB.Name} {
		cronJobName := dpv1alpha1.BuildCronJobName(policy.Name, storageName)
		var cronJob batchv1.CronJob
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: cronJobName}, &cronJob); err != nil {
			t.Fatalf("expected cronjob %s: %v", cronJobName, err)
		}
		if len(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers) != 1 {
			t.Fatalf("expected trigger cronjob %s to have one container", cronJobName)
		}
		args := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args
		if len(args) < 4 || args[0] != "trigger-backup-run" {
			t.Fatalf("expected trigger args for cronjob %s, got %#v", cronJobName, args)
		}
	}

	var updatedPolicy dpv1alpha1.BackupPolicy
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(policy), &updatedPolicy); err != nil {
		t.Fatalf("get updated policy: %v", err)
	}
	if updatedPolicy.Status.Phase != dpv1alpha1.ResourcePhaseReady {
		t.Fatalf("expected policy Ready, got %s", updatedPolicy.Status.Phase)
	}
	if len(updatedPolicy.Status.CronJobNames) != 2 {
		t.Fatalf("expected 2 cronjob names, got %d", len(updatedPolicy.Status.CronJobNames))
	}
}

func TestBackupRunReconcileCreatesJobsAndSnapshotsPerStorage(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := newMySQLSource("backup-system", "mysql-prod")
	storageA := newNFSStorage("backup-system", "nfs-a", "/exports/backups")
	storageB := newS3Storage("backup-system", "minio-b", "platform")
	run := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef: corev1.LocalObjectReference{Name: source.Name},
			StorageRefs: []corev1.LocalObjectReference{
				{Name: storageA.Name},
				{Name: storageB.Name},
			},
			Reason: "manual smoke test",
		},
	}

	fakeClient := newFakeClient(t, scheme, source, storageA, storageB, run)
	reconciler := &BackupRunReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	for _, storageName := range []string{storageA.Name, storageB.Name} {
		jobName := dpv1alpha1.BuildJobName(run.Name, storageName)
		var job batchv1.Job
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: jobName}, &job); err != nil {
			t.Fatalf("expected job %s: %v", jobName, err)
		}
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{{
			Type:               batchv1.JobComplete,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		}}
		if err := fakeClient.Status().Update(ctx, &job); err != nil {
			t.Fatalf("update job status %s: %v", jobName, err)
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
	if len(updatedRun.Status.Storages) != 2 {
		t.Fatalf("expected 2 storage statuses, got %d", len(updatedRun.Status.Storages))
	}

	expectedPath := backupArtifactPath(source, nil, run)
	for _, storageName := range []string{storageA.Name, storageB.Name} {
		snapshotName := dpv1alpha1.BuildSnapshotName(run.Name, storageName, "snapshot")
		var snapshot dpv1alpha1.Snapshot
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: run.Namespace, Name: snapshotName}, &snapshot); err != nil {
			t.Fatalf("expected snapshot %s: %v", snapshotName, err)
		}
		if snapshot.Spec.StorageRef.Name != storageName {
			t.Fatalf("expected snapshot %s to reference storage %s, got %s", snapshotName, storageName, snapshot.Spec.StorageRef.Name)
		}
		if snapshot.Spec.StoragePath != expectedPath {
			t.Fatalf("expected snapshot path %s, got %s", expectedPath, snapshot.Spec.StoragePath)
		}
	}
}

func TestRestoreRequestReconcileUsesSnapshotStoragePath(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := newMySQLSource("backup-system", "mysql-prod")
	storage := newNFSStorage("backup-system", "nfs-a", "/exports/backups")
	backupRun := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-001", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			SourceRef:   corev1.LocalObjectReference{Name: source.Name},
			StorageRefs: []corev1.LocalObjectReference{{Name: storage.Name}},
			Snapshot:    "snapshot-001",
		},
		Status: dpv1alpha1.BackupRunStatus{
			Storages: []dpv1alpha1.StorageRunStatus{{
				Name:        storage.Name,
				StoragePath: "backups/mysql/backup-system/mysql-prod/runs/manual-001",
				Snapshot:    "snapshot-001",
				Phase:       dpv1alpha1.ResourcePhaseSucceeded,
			}},
		},
	}
	snapshot := &dpv1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: dpv1alpha1.BuildSnapshotName(backupRun.Name, storage.Name, "snapshot"), Namespace: "backup-system", UID: types.UID("snapshot-uid")},
		Spec: dpv1alpha1.SnapshotSpec{
			SourceRef:    corev1.LocalObjectReference{Name: source.Name},
			BackupRunRef: corev1.LocalObjectReference{Name: backupRun.Name},
			StorageRef:   corev1.LocalObjectReference{Name: storage.Name},
			StoragePath:  "backups/mysql/backup-system/mysql-prod/runs/manual-001",
			Driver:       dpv1alpha1.BackupDriverMySQL,
			Snapshot:     "snapshot-001",
		},
		Status: dpv1alpha1.SnapshotStatus{Phase: dpv1alpha1.ResourcePhaseSucceeded},
	}
	restore := &dpv1alpha1.RestoreRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-by-snapshot", Namespace: "backup-system", UID: types.UID("restore-uid")},
		Spec: dpv1alpha1.RestoreRequestSpec{
			SourceRef:   corev1.LocalObjectReference{Name: source.Name},
			SnapshotRef: &corev1.LocalObjectReference{Name: snapshot.Name},
			Target:      dpv1alpha1.RestoreTargetSpec{Mode: dpv1alpha1.RestoreTargetModeInPlace},
		},
	}

	fakeClient := newFakeClient(t, scheme, source, storage, backupRun, snapshot, restore)
	reconciler := &RestoreRequestReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(restore)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	jobName := dpv1alpha1.BuildJobName(restore.Name, "restore")
	var job batchv1.Job
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: jobName}, &job); err != nil {
		t.Fatalf("expected restore job %s: %v", jobName, err)
	}
	if got := job.Annotations[snapshotAnnotation]; got != "snapshot-001.sql.gz" {
		t.Fatalf("expected snapshot annotation snapshot-001.sql.gz, got %q", got)
	}
	if got := job.Annotations[storagePathAnnotation]; got != snapshot.Spec.StoragePath {
		t.Fatalf("expected storage path annotation %q, got %q", snapshot.Spec.StoragePath, got)
	}
}

func newMySQLSource(namespace, name string) *dpv1alpha1.BackupSource {
	return &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(name + "-uid")},
		Spec: dpv1alpha1.BackupSourceSpec{
			Driver: dpv1alpha1.BackupDriverMySQL,
			Endpoint: dpv1alpha1.EndpointSpec{
				Host: "mysql.default.svc",
				Port: 3306,
			},
		},
	}
}

func newNFSStorage(namespace, name, path string) *dpv1alpha1.BackupStorage {
	return &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(name + "-uid")},
		Spec: dpv1alpha1.BackupStorageSpec{
			Type: dpv1alpha1.StorageTypeNFS,
			NFS:  &dpv1alpha1.NFSStorageSpec{Server: "10.0.0.10", Path: path},
		},
	}
}

func newS3Storage(namespace, name, prefix string) *dpv1alpha1.BackupStorage {
	return &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(name + "-uid")},
		Spec: dpv1alpha1.BackupStorageSpec{
			Type: dpv1alpha1.StorageTypeS3,
			S3: &dpv1alpha1.S3StorageSpec{
				Endpoint: "https://minio.example.com",
				Bucket:   "data-protection",
				Prefix:   prefix,
			},
		},
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
