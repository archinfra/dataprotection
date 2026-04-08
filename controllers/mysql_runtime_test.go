package controllers

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func TestMySQLS3UploadScriptCreatesBucketWhenMissing(t *testing.T) {
	if !strings.Contains(mysqlS3UploadScript, `mc_cmd mb "backup/${S3_BUCKET}"`) {
		t.Fatalf("expected s3 upload script to create the bucket when it is missing")
	}
	if !strings.Contains(mysqlS3UploadScript, `mc_cmd stat "backup/${remote_path}/snapshots/${snapshot_name}"`) {
		t.Fatalf("expected s3 upload script to verify the uploaded snapshot on the remote backend")
	}
	if !strings.Contains(mysqlS3UploadScript, `if [ "${snapshot_kind}" = "directory" ]; then`) {
		t.Fatalf("expected s3 upload verification to support directory snapshots for addon-backed object storage sources")
	}
}

func TestMySQLScriptsUseMtimeBasedSnapshotOrdering(t *testing.T) {
	if !strings.Contains(mysqlBackupScript, `find "${snapshot_dir}" -maxdepth 1 -type f -name "*.sql.gz" -printf '%T@ %f\n' | sort -nr | awk '{print $2}'`) {
		t.Fatalf("expected mysql backup pruning to sort snapshots by modification time")
	}
	if !strings.Contains(mysqlRestoreScript, `find "${snapshot_dir}" -maxdepth 1 -type f -name "*.sql.gz" -printf '%T@ %f\n' | sort -nr | awk 'NR==1 {print $2}'`) {
		t.Fatalf("expected mysql restore latest fallback to use modification time ordering")
	}
}

func TestMySQLBackupScriptRecordsStorageName(t *testing.T) {
	if !strings.Contains(mysqlBackupScript, `echo "storage=${DP_STORAGE_NAME:-}"`) {
		t.Fatalf("expected mysql backup metadata to record the storage name")
	}
	if strings.Contains(mysqlBackupScript, `DP_REPOSITORY_NAME`) {
		t.Fatalf("did not expect deprecated repository metadata in mysql backup script")
	}
}

func TestDefaultImagePullPolicyUsesAlwaysForMutableTags(t *testing.T) {
	if got := defaultImagePullPolicy("sealos.hub:5000/kube4/dataprotection-operator:latest"); got != corev1.PullAlways {
		t.Fatalf("expected mutable latest tag to default to Always, got %s", got)
	}
	if got := defaultImagePullPolicy("mysql:8.0.45"); got != corev1.PullIfNotPresent {
		t.Fatalf("expected immutable versioned tag to default to IfNotPresent, got %s", got)
	}
}

func TestResolveBuiltInAddonFindsStaticAddonImplementations(t *testing.T) {
	for driver, expected := range map[dpv1alpha1.BackupDriver]string{
		dpv1alpha1.BackupDriverMySQL: "mysql",
		dpv1alpha1.BackupDriverRedis: "redis",
		dpv1alpha1.BackupDriverMinIO: "minio",
	} {
		addon := resolveBuiltInAddon(driver, dpv1alpha1.ExecutionTemplateSpec{})
		if addon == nil {
			t.Fatalf("expected built-in addon for driver %q", driver)
		}
		if addon.Name() != expected {
			t.Fatalf("expected addon %q for driver %q, got %q", expected, driver, addon.Name())
		}
	}
}
