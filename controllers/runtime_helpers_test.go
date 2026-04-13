package controllers

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func TestAddonWrappedCommandLeavesRoomForDoneMarker(t *testing.T) {
	script := addonWrappedCommand([]string{"/bin/true"}, nil, "/workspace", "/workspace/status/plugin.done", "/workspace/status/plugin.failed")

	if strings.Contains(script, "exec '/bin/true'") || strings.Contains(script, "exec /bin/true") {
		t.Fatalf("expected wrapped command to avoid exec so the done marker can be written, got script:\n%s", script)
	}
	if !strings.Contains(script, "touch '/workspace/status/plugin.done'") {
		t.Fatalf("expected wrapped command to touch the done marker, got script:\n%s", script)
	}
}

func TestBuildBackupJobSpecSetsDefaultActiveDeadline(t *testing.T) {
	t.Setenv("DP_DEFAULT_JOB_ACTIVE_DEADLINE_SECONDS", "")

	spec, _, _, err := buildBackupJobSpec(
		"BackupJob",
		"mysql-smoke-manual",
		"series/test",
		"series/test/storage/nfs",
		dpv1alpha1.JobRuntimeSpec{},
		nil,
		testBackupSource(),
		testBackupAddon(),
		testBackupStorage(),
		3,
		"",
	)
	if err != nil {
		t.Fatalf("buildBackupJobSpec returned error: %v", err)
	}
	if spec.ActiveDeadlineSeconds == nil {
		t.Fatal("expected backup job spec to set activeDeadlineSeconds by default")
	}
	if got, want := *spec.ActiveDeadlineSeconds, int64(1800); got != want {
		t.Fatalf("expected default activeDeadlineSeconds %d, got %d", want, got)
	}
}

func TestBuildBackupJobSpecRespectsExplicitActiveDeadline(t *testing.T) {
	override := int64(420)
	spec, _, _, err := buildBackupJobSpec(
		"BackupJob",
		"mysql-smoke-manual",
		"series/test",
		"series/test/storage/nfs",
		dpv1alpha1.JobRuntimeSpec{ActiveDeadlineSeconds: &override},
		nil,
		testBackupSource(),
		testBackupAddon(),
		testBackupStorage(),
		3,
		"",
	)
	if err != nil {
		t.Fatalf("buildBackupJobSpec returned error: %v", err)
	}
	if spec.ActiveDeadlineSeconds == nil {
		t.Fatal("expected explicit activeDeadlineSeconds to be preserved")
	}
	if got := *spec.ActiveDeadlineSeconds; got != override {
		t.Fatalf("expected activeDeadlineSeconds %d, got %d", override, got)
	}
}

func TestBuildBackupJobSpecSharesExecutionEnvWithPackagingStages(t *testing.T) {
	spec, _, _, err := buildBackupJobSpec(
		"BackupPolicy",
		"mysql-smoke-minio",
		"source/backup-system/mysql-smoke/policy/mysql-smoke-minio/storage/minio-primary",
		"series/backup-system/mysql-smoke/policy/mysql-smoke-minio/storage/minio-primary",
		dpv1alpha1.JobRuntimeSpec{},
		nil,
		testBackupSource(),
		testBackupAddon(),
		testMinIOBackupStorage(),
		3,
		"",
	)
	if err != nil {
		t.Fatalf("buildBackupJobSpec returned error: %v", err)
	}

	packager := findContainerByName(t, spec.Template.Spec.Containers, "artifact-package")
	uploader := findContainerByName(t, spec.Template.Spec.Containers, "artifact-upload")

	assertEnvVarPresent(t, packager.Env, "DP_EXECUTION_SLUG", "mysql-smoke-minio")
	assertEnvVarPresent(t, packager.Env, "DP_SERIES", "source/backup-system/mysql-smoke/policy/mysql-smoke-minio/storage/minio-primary")
	assertEnvVarPresent(t, uploader.Env, "DP_BACKEND_PATH", "series/backup-system/mysql-smoke/policy/mysql-smoke-minio/storage/minio-primary")
	assertEnvVarPresent(t, uploader.Env, "DP_KEEP_LAST", "3")
}

func TestBuildArtifactPackageContainerMarksFailureOnPackagingError(t *testing.T) {
	container := buildArtifactPackageContainer([]corev1.EnvVar{{Name: "DP_EXECUTION_SLUG", Value: "mysql-smoke"}})
	if len(container.Args) != 1 {
		t.Fatalf("expected exactly one script arg, got %d", len(container.Args))
	}
	script := container.Args[0]
	if !strings.Contains(script, "trap 'touch /workspace/status/package.failed' ERR") {
		t.Fatalf("expected package container to mark package.failed on errors, got script:\n%s", script)
	}
}

func testBackupSource() *dpv1alpha1.BackupSource {
	return &dpv1alpha1.BackupSource{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-smoke", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupSourceSpec{
			AddonRef: corev1.LocalObjectReference{Name: "mysql-dump"},
			Endpoint: dpv1alpha1.EndpointSpec{
				Host:     "mysql.default.svc",
				Port:     3306,
				Username: "root",
			},
			Parameters: map[string]string{
				"database": "demo",
			},
		},
	}
}

func testBackupAddon() *dpv1alpha1.BackupAddon {
	return &dpv1alpha1.BackupAddon{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-dump"},
		Spec: dpv1alpha1.BackupAddonSpec{
			BackupTemplate: dpv1alpha1.AddonTemplateSpec{
				Image:   "example/mysql-addon:latest",
				Command: []string{"/bin/true"},
			},
		},
	}
}

func testBackupStorage() *dpv1alpha1.BackupStorage {
	return &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-primary", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupStorageSpec{
			Type: dpv1alpha1.StorageTypeNFS,
			NFS: &dpv1alpha1.NFSStorageSpec{
				Server: "10.0.0.10",
				Path:   "/exports/backups",
			},
		},
	}
}

func testMinIOBackupStorage() *dpv1alpha1.BackupStorage {
	return &dpv1alpha1.BackupStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "minio-primary", Namespace: "backup-system"},
		Spec: dpv1alpha1.BackupStorageSpec{
			Type: dpv1alpha1.StorageTypeMinIO,
			MinIO: &dpv1alpha1.MinIOStorageSpec{
				Endpoint:         "http://minio.minio-backup.svc.cluster.local:9000",
				Bucket:           "data-protection",
				Prefix:           "aict",
				AutoCreateBucket: true,
			},
		},
	}
}

func findContainerByName(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, container := range containers {
		if container.Name == name {
			return container
		}
	}
	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}

func assertEnvVarPresent(t *testing.T, env []corev1.EnvVar, name, want string) {
	t.Helper()
	for _, item := range env {
		if item.Name == name {
			if item.Value != want {
				t.Fatalf("env %s expected %q, got %q", name, want, item.Value)
			}
			return
		}
	}
	var names []string
	for _, item := range env {
		names = append(names, fmt.Sprintf("%s=%s", item.Name, item.Value))
	}
	t.Fatalf("env %s not found, got %v", name, names)
}
