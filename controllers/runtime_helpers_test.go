package controllers

import (
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
