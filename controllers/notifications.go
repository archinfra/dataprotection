package controllers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type NotificationEvent struct {
	Type          string            `json:"type"`
	Namespace     string            `json:"namespace"`
	ResourceKind  string            `json:"resourceKind"`
	ResourceName  string            `json:"resourceName"`
	Phase         string            `json:"phase"`
	Message       string            `json:"message"`
	SourceName    string            `json:"sourceName,omitempty"`
	StorageName   string            `json:"storageName,omitempty"`
	SnapshotName  string            `json:"snapshotName,omitempty"`
	NativeJobName string            `json:"nativeJobName,omitempty"`
	Series        string            `json:"series,omitempty"`
	Timestamp     string            `json:"timestamp"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type NotificationTarget struct {
	Name                  string            `json:"name"`
	URL                   string            `json:"url"`
	Method                string            `json:"method"`
	Headers               map[string]string `json:"headers,omitempty"`
	TimeoutSeconds        int32             `json:"timeoutSeconds,omitempty"`
	InsecureSkipTLSVerify bool              `json:"insecureSkipTlsVerify,omitempty"`
}

type NotificationDispatchRequest struct {
	Event   NotificationEvent    `json:"event"`
	Targets []NotificationTarget `json:"targets"`
}

type NotificationDispatchResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

type NotificationDispatchResponse struct {
	Success bool                         `json:"success"`
	Message string                       `json:"message,omitempty"`
	Results []NotificationDispatchResult `json:"results,omitempty"`
}

func dispatchNotifications(ctx context.Context, c client.Client, namespace string, refs []corev1.LocalObjectReference, event NotificationEvent) (dpv1alpha1.NotificationDeliveryStatus, error) {
	status := dpv1alpha1.NotificationDeliveryStatus{}
	if len(refs) == 0 {
		return status, nil
	}
	targets, err := resolveNotificationTargets(ctx, c, namespace, refs)
	if err != nil {
		status.Phase = dpv1alpha1.NotificationDeliveryFailed
		status.Attempts = 1
		status.LastAttemptTime = nowTime()
		status.Message = err.Error()
		return status, err
	}
	requestBody, err := json.Marshal(NotificationDispatchRequest{Event: event, Targets: targets})
	if err != nil {
		status.Phase = dpv1alpha1.NotificationDeliveryFailed
		status.Attempts = 1
		status.LastAttemptTime = nowTime()
		status.Message = err.Error()
		return status, err
	}
	gatewayURL := os.Getenv("DP_NOTIFICATION_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = defaultNotificationGatewayURL()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL+"/dispatch", bytes.NewReader(requestBody))
	if err != nil {
		status.Phase = dpv1alpha1.NotificationDeliveryFailed
		status.Attempts = 1
		status.LastAttemptTime = nowTime()
		status.Message = err.Error()
		return status, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	status.Attempts = 1
	status.LastAttemptTime = nowTime()
	if err != nil {
		status.Phase = dpv1alpha1.NotificationDeliveryFailed
		status.Message = err.Error()
		return status, err
	}
	defer resp.Body.Close()

	var response NotificationDispatchResponse
	_ = json.NewDecoder(resp.Body).Decode(&response)
	if resp.StatusCode >= 300 || !response.Success {
		status.Phase = dpv1alpha1.NotificationDeliveryFailed
		if response.Message != "" {
			status.Message = response.Message
		} else {
			status.Message = fmt.Sprintf("notification gateway returned %s", resp.Status)
		}
		return status, fmt.Errorf(status.Message)
	}
	status.Phase = dpv1alpha1.NotificationDeliverySucceeded
	status.LastDeliveredTime = nowTime()
	status.Message = response.Message
	return status, nil
}

func backupNotificationType(observation *terminalBackupObservation) string {
	if observation == nil {
		return "BackupUnknown"
	}
	if observation.Phase == dpv1alpha1.ResourcePhaseSucceeded {
		return "BackupSucceeded"
	}
	if observation.StorageProbeResult == dpv1alpha1.ProbeResultFailed {
		return "StorageProbeFailed"
	}
	return "BackupFailed"
}

func restoreNotificationType(observation *terminalRestoreObservation) string {
	if observation == nil {
		return "RestoreUnknown"
	}
	if observation.Phase == dpv1alpha1.ResourcePhaseSucceeded {
		return "RestoreSucceeded"
	}
	if observation.StorageProbeResult == dpv1alpha1.ProbeResultFailed {
		return "StorageProbeFailed"
	}
	return "RestoreFailed"
}

func resolveNotificationTargets(ctx context.Context, c client.Client, namespace string, refs []corev1.LocalObjectReference) ([]NotificationTarget, error) {
	targets := make([]NotificationTarget, 0, len(refs))
	for _, ref := range refs {
		endpoint, err := getNotificationEndpoint(ctx, c, namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		headers := map[string]string{}
		for key, value := range endpoint.Spec.Headers {
			headers[key] = value
		}
		for _, secretHeader := range endpoint.Spec.SecretHeaders {
			var secret corev1.Secret
			if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretHeader.SecretKeyRef.Name}, &secret); err != nil {
				return nil, err
			}
			value, ok := secret.Data[secretHeader.SecretKeyRef.Key]
			if !ok {
				return nil, fmt.Errorf("notification secret %s/%s key %s not found", namespace, secretHeader.SecretKeyRef.Name, secretHeader.SecretKeyRef.Key)
			}
			headers[secretHeader.Name] = string(value)
		}
		method := endpoint.Spec.Method
		if method == "" {
			method = http.MethodPost
		}
		timeout := endpoint.Spec.TimeoutSeconds
		if timeout <= 0 {
			timeout = 10
		}
		targets = append(targets, NotificationTarget{
			Name:                  endpoint.Name,
			URL:                   endpoint.Spec.URL,
			Method:                method,
			Headers:               headers,
			TimeoutSeconds:        timeout,
			InsecureSkipTLSVerify: endpoint.Spec.InsecureSkipTLSVerify,
		})
	}
	return targets, nil
}

func deliverToTarget(ctx context.Context, target NotificationTarget, event NotificationEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range target.Headers {
		req.Header.Set(key, value)
	}
	timeout := time.Duration(target.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: target.InsecureSkipTLSVerify},
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("target %s returned %s", target.Name, resp.Status)
	}
	return nil
}
