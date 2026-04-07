package controllers

import (
	"os"
	"strings"
)

const (
	defaultPlaceholderRunnerImageValue = "busybox:1.36"
	defaultMySQLRunnerImageValue       = "mysql:8.0.45"
	defaultS3HelperImageValue          = "minio/mc:latest"
)

func defaultPlaceholderRunnerImage() string {
	return envOrDefault("DP_DEFAULT_RUNNER_IMAGE", defaultPlaceholderRunnerImageValue)
}

func defaultMySQLRunnerImage() string {
	return envOrDefault("DP_DEFAULT_MYSQL_RUNNER_IMAGE", defaultMySQLRunnerImageValue)
}

func defaultS3HelperImage() string {
	return envOrDefault("DP_DEFAULT_S3_HELPER_IMAGE", defaultS3HelperImageValue)
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
