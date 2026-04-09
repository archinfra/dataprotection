package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/archinfra/dataprotection/controllers"
)

func runNotificationGateway(args []string) error {
	fs := flag.NewFlagSet("notification-gateway", flag.ContinueOnError)
	listenAddr := os.Getenv("DP_NOTIFICATION_GATEWAY_LISTEN_ADDRESS")
	if listenAddr == "" {
		listenAddr = ":8090"
	}
	fs.StringVar(&listenAddr, "listen-address", listenAddr, "The address the notification gateway binds to.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/dispatch", handleNotificationDispatch)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return server.ListenAndServe()
}

func handleNotificationDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var request controllers.NotificationDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response := controllers.NotificationDispatchResponse{Success: true}
	for _, target := range request.Targets {
		result := controllers.NotificationDispatchResult{Name: target.Name}
		if err := deliverNotification(r.Context(), target, request.Event); err != nil {
			result.Success = false
			result.Message = err.Error()
			response.Success = false
		} else {
			result.Success = true
			result.Message = "delivered"
		}
		response.Results = append(response.Results, result)
	}
	if response.Success {
		response.Message = "all notifications delivered"
		w.WriteHeader(http.StatusOK)
	} else {
		response.Message = "one or more notifications failed"
		w.WriteHeader(http.StatusBadGateway)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func deliverNotification(ctx context.Context, target controllers.NotificationTarget, event controllers.NotificationEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	method := target.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, target.URL, bytes.NewReader(payload))
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
