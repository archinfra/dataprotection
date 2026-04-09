package controllers

import (
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	defaultUtilityImageValue             = "registry.cn-beijing.aliyuncs.com/kube4/busybox:v1"
	defaultMinIOHelperImageValue         = "minio/mc:latest"
	defaultNotificationGatewayListenAddr = ":8090"
	defaultNotificationGatewayURLValue   = "http://data-protection-notification-gateway.data-protection-system.svc.cluster.local:8090"
	defaultControllerImageValue          = "sealos.hub:5000/kube4/dataprotection-operator:latest"
	defaultJobBackoffLimitValue          = int32(0)
	defaultJobTTLSecondsValue            = int32(86400)
	defaultCronJobSuccessHistoryValue    = int32(1)
	defaultCronJobFailedHistoryValue     = int32(1)
)

func defaultUtilityImage() string {
	return envOrDefault("DP_DEFAULT_UTILITY_IMAGE", defaultUtilityImageValue)
}

func defaultMinIOHelperImage() string {
	return envOrDefault("DP_DEFAULT_MINIO_HELPER_IMAGE", defaultMinIOHelperImageValue)
}

func defaultNotificationGatewayURL() string {
	return envOrDefault("DP_NOTIFICATION_GATEWAY_URL", defaultNotificationGatewayURLValue)
}

func defaultNotificationGatewayListenAddress() string {
	return envOrDefault("DP_NOTIFICATION_GATEWAY_LISTEN_ADDRESS", defaultNotificationGatewayListenAddr)
}

func defaultControllerImage() string {
	return envOrDefault("DP_DEFAULT_CONTROLLER_IMAGE", defaultControllerImageValue)
}

func defaultImagePullPolicy(images ...string) corev1.PullPolicy {
	for _, image := range images {
		if imageUsesMutableTag(image) {
			return corev1.PullAlways
		}
	}
	return corev1.PullIfNotPresent
}

func defaultJobTTLSeconds() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_JOB_TTL_SECONDS", defaultJobTTLSecondsValue)
	return &value
}

func defaultJobBackoffLimit() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_JOB_BACKOFF_LIMIT", defaultJobBackoffLimitValue)
	return &value
}

func defaultCronJobSuccessfulHistoryLimit() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_CRONJOB_SUCCESS_HISTORY", defaultCronJobSuccessHistoryValue)
	return &value
}

func defaultCronJobFailedHistoryLimit() *int32 {
	value := envOrDefaultInt32("DP_DEFAULT_CRONJOB_FAILED_HISTORY", defaultCronJobFailedHistoryValue)
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

func imageUsesMutableTag(image string) bool {
	image = strings.TrimSpace(image)
	if image == "" {
		return false
	}
	if strings.Contains(image, "@sha256:") {
		return false
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return true
	}
	tag := strings.TrimSpace(image[lastColon+1:])
	return tag == "" || tag == "latest"
}
