package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestBackupExecutionSpecValidateBasic(t *testing.T) {
	tests := []struct {
		name    string
		spec    BackupExecutionSpec
		wantErr bool
	}{
		{
			name: "valid manual execution",
			spec: BackupExecutionSpec{
				SourceRef:  corev1.LocalObjectReference{Name: "mysql-prod"},
				StorageRef: corev1.LocalObjectReference{Name: "nfs-primary"},
				Trigger:    BackupExecutionTriggerManual,
			},
		},
		{
			name: "valid scheduled execution",
			spec: BackupExecutionSpec{
				PolicyRef:  &corev1.LocalObjectReference{Name: "mysql-daily"},
				SourceRef:  corev1.LocalObjectReference{Name: "mysql-prod"},
				StorageRef: corev1.LocalObjectReference{Name: "minio-primary"},
				Trigger:    BackupExecutionTriggerScheduled,
			},
		},
		{
			name: "missing source",
			spec: BackupExecutionSpec{
				StorageRef: corev1.LocalObjectReference{Name: "nfs-primary"},
			},
			wantErr: true,
		},
		{
			name: "missing storage",
			spec: BackupExecutionSpec{
				SourceRef: corev1.LocalObjectReference{Name: "mysql-prod"},
			},
			wantErr: true,
		},
		{
			name: "invalid trigger",
			spec: BackupExecutionSpec{
				SourceRef:  corev1.LocalObjectReference{Name: "mysql-prod"},
				StorageRef: corev1.LocalObjectReference{Name: "nfs-primary"},
				Trigger:    BackupExecutionTrigger("Unknown"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.ValidateBasic()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateBasic() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
