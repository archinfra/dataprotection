package controllers

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

	triggerServiceAccount := triggerServiceAccountName(policy.Name)
	var serviceAccount corev1.ServiceAccount
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: triggerServiceAccount}, &serviceAccount); err != nil {
		t.Fatalf("expected trigger service account %s: %v", triggerServiceAccount, err)
	}

	roleName := triggerRoleName(policy.Name)
	var role rbacv1.Role
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: roleName}, &role); err != nil {
		t.Fatalf("expected trigger role %s: %v", roleName, err)
	}
	if len(role.Rules) != 2 {
		t.Fatalf("expected 2 trigger policy rules, got %d", len(role.Rules))
	}

	roleBindingName := triggerRoleBindingName(policy.Name)
	var roleBinding rbacv1.RoleBinding
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: roleBindingName}, &roleBinding); err != nil {
		t.Fatalf("expected trigger rolebinding %s: %v", roleBindingName, err)
	}
	if len(roleBinding.Subjects) != 1 || roleBinding.Subjects[0].Name != triggerServiceAccount {
		t.Fatalf("expected trigger rolebinding to point at service account %s, got %#v", triggerServiceAccount, roleBinding.Subjects)
	}

	for _, storageName := range []string{storageA.Name, storageB.Name} {
		cronJobName := dpv1alpha1.BuildCronJobName(policy.Name, storageName)
		var cronJob batchv1.CronJob
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: cronJobName}, &cronJob); err != nil {
			t.Fatalf("expected cronjob %s: %v", cronJobName, err)
		}
		if cronJob.Spec.SuccessfulJobsHistoryLimit == nil || *cronJob.Spec.SuccessfulJobsHistoryLimit != 1 {
			t.Fatalf("expected trigger success history limit 1 for cronjob %s", cronJobName)
		}
		if cronJob.Spec.FailedJobsHistoryLimit == nil || *cronJob.Spec.FailedJobsHistoryLimit != 1 {
			t.Fatalf("expected trigger failed history limit 1 for cronjob %s", cronJobName)
		}
		if cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName != triggerServiceAccount {
			t.Fatalf("expected trigger service account %s for cronjob %s, got %s", triggerServiceAccount, cronJobName, cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName)
		}
		if cronJob.Spec.JobTemplate.Spec.Parallelism == nil || *cronJob.Spec.JobTemplate.Spec.Parallelism != 1 {
			t.Fatalf("expected trigger job template parallelism 1 for cronjob %s", cronJobName)
		}
		if cronJob.Spec.JobTemplate.Spec.Completions == nil || *cronJob.Spec.JobTemplate.Spec.Completions != 1 {
			t.Fatalf("expected trigger job template completions 1 for cronjob %s", cronJobName)
		}
		if cronJob.Spec.JobTemplate.Spec.PodReplacementPolicy == nil || *cronJob.Spec.JobTemplate.Spec.PodReplacementPolicy != batchv1.Failed {
			t.Fatalf("expected trigger job template podReplacementPolicy Failed for cronjob %s", cronJobName)
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
		if job.Spec.Parallelism == nil || *job.Spec.Parallelism != 1 {
			t.Fatalf("expected job %s parallelism 1", jobName)
		}
		if job.Spec.Completions == nil || *job.Spec.Completions != 1 {
			t.Fatalf("expected job %s completions 1", jobName)
		}
		if job.Spec.PodReplacementPolicy == nil || *job.Spec.PodReplacementPolicy != batchv1.Failed {
			t.Fatalf("expected job %s podReplacementPolicy Failed", jobName)
		}
		if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 86400 {
			t.Fatalf("expected default ttlSecondsAfterFinished 86400 for job %s", jobName)
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

func TestBackupRunReconcilePrunesOldSnapshotsAndMarksLatest(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	source := newMySQLSource("backup-system", "mysql-prod")
	storage := newS3Storage("backup-system", "minio-primary", "smoke")
	policy := &dpv1alpha1.BackupPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-daily", Namespace: "backup-system", UID: types.UID("policy-uid")},
		Spec: dpv1alpha1.BackupPolicySpec{
			SourceRef:   corev1.LocalObjectReference{Name: source.Name},
			StorageRefs: []corev1.LocalObjectReference{{Name: storage.Name}},
			Schedule:    dpv1alpha1.BackupScheduleSpec{Cron: "*/5 * * * *"},
			Retention:   dpv1alpha1.RetentionRule{KeepLast: 2},
		},
	}
	run := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "scheduled-new", Namespace: "backup-system", UID: types.UID("run-uid")},
		Spec: dpv1alpha1.BackupRunSpec{
			PolicyRef:   &corev1.LocalObjectReference{Name: policy.Name},
			SourceRef:   corev1.LocalObjectReference{Name: source.Name},
			StorageRefs: []corev1.LocalObjectReference{{Name: storage.Name}},
		},
	}

	storagePath := backupArtifactPath(source, policy, run)
	oldSnapshotA := &dpv1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "scheduled-old-a-minio-primary-snapshot", Namespace: "backup-system"},
		Spec: dpv1alpha1.SnapshotSpec{
			SourceRef:    corev1.LocalObjectReference{Name: source.Name},
			BackupRunRef: corev1.LocalObjectReference{Name: "scheduled-old-a"},
			StorageRef:   corev1.LocalObjectReference{Name: storage.Name},
			StoragePath:  storagePath,
			Driver:       dpv1alpha1.BackupDriverMySQL,
			Snapshot:     "scheduled-old-a.sql.gz",
		},
		Status: dpv1alpha1.SnapshotStatus{
			Phase:       dpv1alpha1.ResourcePhaseSucceeded,
			CompletedAt: &metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
		},
	}
	oldSnapshotB := &dpv1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "scheduled-old-b-minio-primary-snapshot", Namespace: "backup-system"},
		Spec: dpv1alpha1.SnapshotSpec{
			SourceRef:    corev1.LocalObjectReference{Name: source.Name},
			BackupRunRef: corev1.LocalObjectReference{Name: "scheduled-old-b"},
			StorageRef:   corev1.LocalObjectReference{Name: storage.Name},
			StoragePath:  storagePath,
			Driver:       dpv1alpha1.BackupDriverMySQL,
			Snapshot:     "scheduled-old-b.sql.gz",
		},
		Status: dpv1alpha1.SnapshotStatus{
			Phase:       dpv1alpha1.ResourcePhaseSucceeded,
			CompletedAt: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
		},
	}

	fakeClient := newFakeClient(t, scheme, source, storage, policy, run, oldSnapshotA, oldSnapshotB)
	reconciler := &BackupRunReconciler{Client: fakeClient, Scheme: scheme}

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	jobName := dpv1alpha1.BuildJobName(run.Name, storage.Name)
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

	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	var snapshots dpv1alpha1.SnapshotList
	if err := fakeClient.List(ctx, &snapshots, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list snapshots: %v", err)
	}

	if len(snapshots.Items) != 2 {
		t.Fatalf("expected 2 retained snapshots after pruning, got %d", len(snapshots.Items))
	}

	currentName := dpv1alpha1.BuildSnapshotName(run.Name, storage.Name, "snapshot")
	foundCurrent := false
	foundPrevious := false
	for _, snapshot := range snapshots.Items {
		switch snapshot.Name {
		case currentName:
			foundCurrent = true
			if !snapshot.Status.Latest {
				t.Fatalf("expected current snapshot %s to be marked latest", snapshot.Name)
			}
			if !snapshot.Status.ArtifactReady {
				t.Fatalf("expected current snapshot %s to be artifact-ready", snapshot.Name)
			}
		case oldSnapshotB.Name:
			foundPrevious = true
			if snapshot.Status.Latest {
				t.Fatalf("did not expect retained previous snapshot %s to be latest", snapshot.Name)
			}
		}
	}

	if !foundCurrent {
		t.Fatalf("expected current snapshot %s to remain", currentName)
	}
	if !foundPrevious {
		t.Fatalf("expected newest historical snapshot %s to remain", oldSnapshotB.Name)
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
