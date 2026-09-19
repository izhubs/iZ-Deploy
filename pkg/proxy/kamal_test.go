// Package proxy verifies sidecar process lifecycle management and HTTP control plane integration.
package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHTTPClient_Operations verifies REST API dispatch across deploy, remove, pause, resume, targets, and ping.
//
// Business rule: HTTPClient translates high-level proxy commands into standard JSON REST calls.
//
// @ai-constraint: Ensure request bodies and query parameters match kamal-proxy protocol specifications.
func TestHTTPClient_Operations(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedService string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path

		switch {
		case r.URL.Path == "/deploy" && r.Method == http.MethodPost:
			var payload TargetPayload
			_ = json.NewDecoder(r.Body).Decode(&payload)
			receivedService = payload.Service
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/remove" && r.Method == http.MethodPost:
			var payload TargetPayload
			_ = json.NewDecoder(r.Body).Decode(&payload)
			receivedService = payload.Service
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/pause" && r.Method == http.MethodPost:
			receivedService = r.URL.Query().Get("service")
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/resume" && r.Method == http.MethodPost:
			receivedService = r.URL.Query().Get("service")
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/targets" && r.Method == http.MethodGet:
			targets := []TargetStatus{
				{Service: "app", Target: "127.0.0.1:8080", Host: "app.local", State: "active"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(targets)

		case r.URL.Path == "/up" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Extract port from test server
	parts := strings.Split(ts.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	client := NewHTTPClient(port)
	ctx := context.Background()

	// 1. Deploy test
	deployErr := client.Deploy(ctx, TargetPayload{
		Service: "service-a",
		Target:  "127.0.0.1:3000",
		Host:    "a.example.com",
		TLS:     true,
	})
	if deployErr != nil {
		t.Fatalf("Deploy() failed: %v", deployErr)
	}
	if receivedService != "service-a" || receivedPath != "/deploy" || receivedMethod != http.MethodPost {
		t.Errorf("Deploy() dispatch mismatch: service=%s path=%s method=%s", receivedService, receivedPath, receivedMethod)
	}

	// 2. Remove test
	removeErr := client.Remove(ctx, "service-a", "127.0.0.1:3000")
	if removeErr != nil {
		t.Fatalf("Remove() failed: %v", removeErr)
	}
	if receivedService != "service-a" || receivedPath != "/remove" || receivedMethod != http.MethodPost {
		t.Errorf("Remove() dispatch mismatch: service=%s path=%s method=%s", receivedService, receivedPath, receivedMethod)
	}

	// 3. Pause test
	pauseErr := client.Pause(ctx, "service-a")
	if pauseErr != nil {
		t.Fatalf("Pause() failed: %v", pauseErr)
	}
	if receivedService != "service-a" || receivedPath != "/pause" || receivedMethod != http.MethodPost {
		t.Errorf("Pause() dispatch mismatch: service=%s path=%s method=%s", receivedService, receivedPath, receivedMethod)
	}

	// 4. Resume test
	resumeErr := client.Resume(ctx, "service-a")
	if resumeErr != nil {
		t.Fatalf("Resume() failed: %v", resumeErr)
	}
	if receivedService != "service-a" || receivedPath != "/resume" || receivedMethod != http.MethodPost {
		t.Errorf("Resume() dispatch mismatch: service=%s path=%s method=%s", receivedService, receivedPath, receivedMethod)
	}

	// 5. ListTargets test
	targets, listErr := client.ListTargets(ctx)
	if listErr != nil {
		t.Fatalf("ListTargets() failed: %v", listErr)
	}
	if len(targets) != 1 || targets[0].Service != "app" {
		t.Errorf("ListTargets() mismatch: %+v", targets)
	}

	// 6. Ping test
	pingErr := client.Ping(ctx)
	if pingErr != nil {
		t.Fatalf("Ping() failed: %v", pingErr)
	}
}

// TestProcessManager_DefaultsAndLifecycle verifies process initialization defaults and defensive error checks.
//
// Business rule: ProcessManager defaults to standard ports and protects against invalid operations.
//
// @ai-constraint: Ensure Stop and Reload on inactive process return ErrProxyNotRunning.
func TestProcessManager_DefaultsAndLifecycle(t *testing.T) {
	pm, err := NewProcessManager(SidecarConfig{})
	if err != nil {
		t.Fatalf("NewProcessManager failed: %v", err)
	}

	if pm.cfg.ManagementPort != DefaultManagementPort {
		t.Errorf("expected ManagementPort %d, got %d", DefaultManagementPort, pm.cfg.ManagementPort)
	}
	if pm.cfg.HTTPPort != DefaultHTTPPort {
		t.Errorf("expected HTTPPort %d, got %d", DefaultHTTPPort, pm.cfg.HTTPPort)
	}
	if pm.cfg.HTTPSPort != DefaultHTTPSPort {
		t.Errorf("expected HTTPSPort %d, got %d", DefaultHTTPSPort, pm.cfg.HTTPSPort)
	}
	if pm.cfg.BinaryPath != "kamal-proxy" {
		t.Errorf("expected default BinaryPath kamal-proxy, got %s", pm.cfg.BinaryPath)
	}

	if pm.IsRunning() {
		t.Error("expected process not to be running upon instantiation")
	}
	if pm.PID() != 0 {
		t.Errorf("expected PID 0, got %d", pm.PID())
	}

	ctx := context.Background()

	// Stop when not running
	stopErr := pm.Stop(ctx)
	if !errors.Is(stopErr, ErrProxyNotRunning) {
		t.Errorf("expected ErrProxyNotRunning on Stop, got %v", stopErr)
	}

	// Reload when not running
	reloadErr := pm.Reload(ctx)
	if !errors.Is(reloadErr, ErrProxyNotRunning) {
		t.Errorf("expected ErrProxyNotRunning on Reload, got %v", reloadErr)
	}

	// Start with non-existent binary
	nonExistentPM, _ := NewProcessManager(SidecarConfig{
		BinaryPath: "this-binary-does-not-exist-izdeploy-test",
	})
	startCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	startErr := nonExistentPM.Start(startCtx)
	if startErr == nil {
		t.Error("expected error when starting non-existent binary, got nil")
	}
}
