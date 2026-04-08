package controllers

import (
	"os"
	"strconv"
	"strings"
)

const (
	defaultPlaceholderRunnerImageValue = "busybox:1.36"
	defaultMySQLRunnerImageValue       = "mysql:8.0.45"
	defaultS3HelperImageValue          = "minio/mc:latest"
	defaultControllerImageValue        = "sealos.hub:5000/kube4/dataprotection-operator:latest"
	defaultJobTTLSecondsValue          = int32(86400)
	defaultCronJobSuccessHistoryValue  = int32(1)
	defaultCronJobFailedHistoryValue   = int32(1)
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

func defaultControllerImage() string {
	return envOrDefault("DP_DEFAULT_CONTROLLER_IMAGE", defaultControllerImageValue)
}

func defaultJobTTLSeconds() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_JOB_TTL_SECONDS", defaultJobTTLSecondsValue)
	return &value
}

func defaultCronJobSuccessfulHistoryLimit() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_TRIGGER_SUCCESS_HISTORY", defaultCronJobSuccessHistoryValue)
	return &value
}

func defaultCronJobFailedHistoryLimit() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_TRIGGER_FAILED_HISTORY", defaultCronJobFailedHistoryValue)
	return &value
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envOrDefaultInt32(key string, fallback int32) int32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(parsed)
}
