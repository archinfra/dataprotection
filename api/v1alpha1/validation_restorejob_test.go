package v1alpha1

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestRestoreJobSpecValidateBasicAcceptsSnapshotRef(t *testing.T) {
	spec := RestoreJobSpec{
		SourceRef:   corev1.LocalObjectReference{Name: "mysql-prod"},
		SnapshotRef: corev1.LocalObjectReference{Name: "mysql-prod-snapshot"},
	}

	if err := spec.ValidateBasic(); err != nil {
		t.Fatalf("expected snapshot-based restore to be valid, got %v", err)
	}
}

func TestRestoreJobSpecValidateBasicAcceptsImportSource(t *testing.T) {
	spec := RestoreJobSpec{
		SourceRef: corev1.LocalObjectReference{Name: "mysql-prod"},
		ImportSource: &RestoreImportSource{
			StorageRef: corev1.LocalObjectReference{Name: "minio-primary"},
			Path:       `imports\cluster-a\mysql-prod.tgz`,
			Format:     RestoreArtifactFormatArchive,
		},
	}

	if err := spec.ValidateBasic(); err != nil {
		t.Fatalf("expected import-based restore to be valid, got %v", err)
	}
}

func TestRestoreJobSpecValidateBasicRequiresExactlyOneRestoreSource(t *testing.T) {
	tests := []struct {
		name string
		spec RestoreJobSpec
		want string
	}{
		{
			name: "missing both",
			spec: RestoreJobSpec{
				SourceRef: corev1.LocalObjectReference{Name: "mysql-prod"},
			},
			want: "one of spec.snapshotRef.name or spec.importSource must be set",
		},
		{
			name: "both set",
			spec: RestoreJobSpec{
				SourceRef:    corev1.LocalObjectReference{Name: "mysql-prod"},
				SnapshotRef:  corev1.LocalObjectReference{Name: "mysql-prod-snapshot"},
				ImportSource: &RestoreImportSource{StorageRef: corev1.LocalObjectReference{Name: "nfs-primary"}, Path: "imports/mysql-prod"},
			},
			want: "mutually exclusive",
		},
		{
			name: "import path escapes root",
			spec: RestoreJobSpec{
				SourceRef: corev1.LocalObjectReference{Name: "mysql-prod"},
				ImportSource: &RestoreImportSource{
					StorageRef: corev1.LocalObjectReference{Name: "nfs-primary"},
					Path:       "../outside",
				},
			},
			want: "must stay within the storage root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.ValidateBasic()
			if err == nil {
				t.Fatalf("expected validation error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected validation error containing %q, got %v", tt.want, err)
			}
		})
	}
}
