package v1alpha1

import "testing"

func TestBuildCronJobNameHonorsCronJobLimit(t *testing.T) {
	name := BuildCronJobName("mysql-prod-very-long-policy-name-for-cross-region-fanout", "minio-secondary-repository-for-dr-site")
	if len(name) > 52 {
		t.Fatalf("expected cronjob name length <= 52, got %d (%s)", len(name), name)
	}
}

func TestBuildJobNameHonorsJobLimit(t *testing.T) {
	name := BuildJobName("manual-run-for-a-very-long-production-cluster-name", "repository-with-a-very-long-dr-name")
	if len(name) > 63 {
		t.Fatalf("expected job name length <= 63, got %d (%s)", len(name), name)
	}
}
