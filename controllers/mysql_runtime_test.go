package controllers

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMySQLS3UploadScriptCreatesBucketWhenMissing(t *testing.T) {
	if !strings.Contains(mysqlS3UploadScript, `mc_cmd mb "backup/${S3_BUCKET}"`) {
		t.Fatalf("expected s3 upload script to create the bucket when it is missing")
	}
	if !strings.Contains(mysqlS3UploadScript, `mc_cmd stat "backup/${remote_path}/snapshots/${snapshot_name}"`) {
		t.Fatalf("expected s3 upload script to verify the uploaded snapshot on the remote backend")
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
