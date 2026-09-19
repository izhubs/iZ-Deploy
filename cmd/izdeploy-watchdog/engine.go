// Package main coordinates heartbeat checks, failure threshold evaluation, and alert logging.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Default watchdog operational parameters.
const (
	DefaultFailThreshold        = 10
	DefaultCheckTimeout         = 5 * time.Second
	DefaultHeartbeatEndpoint    = "http://127.0.0.1:8090/healthz"
	DefaultLogFilePath          = "/var/log/izdeploy/watchdog.log"
	DefaultFallbackLogFilePath  = "/tmp/izdeploy-watchdog.log"
	EmergencyLogPermission      = 0644
)

// LivenessChecker verifies connectivity to the Control Plane or local agent daemon.
type LivenessChecker interface {
	Check(ctx context.Context, endpoint, authToken string) error
}

// HTTPLivenessChecker validates HTTP 200 responses against health endpoints.
type HTTPLivenessChecker struct {
	client *http.Client
}

// Check sends a lightweight HTTP GET request to verify heartbeat response.
//
// Business rule: Endpoint must return HTTP 200 OK within timeout to be deemed healthy.
//
// @ai-constraint: Must set explicit timeout to prevent hanging on stalled TCP handshakes.
func (h *HTTPLivenessChecker) Check(ctx context.Context, endpoint, authToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("heartbeat request creation failed: %w", err)
	}

	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	httpClient := h.client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultCheckTimeout}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat probe failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}

// EngineConfig holds parameters for a single watchdog evaluation pass.
type EngineConfig struct {
	Endpoint      string
	AuthToken     string
	FailThreshold int
	StateFilePath string
	LogFilePath   string
	DryRun        bool
	Timeout       time.Duration
}

// Engine executes the single-pass watchdog evaluation loop.
type Engine struct {
	config   EngineConfig
	checker  LivenessChecker
	firewall FirewallController
	state    *State
}

// NewEngine initializes the watchdog execution engine.
func NewEngine(cfg EngineConfig, checker LivenessChecker, firewall FirewallController) *Engine {
	if cfg.FailThreshold <= 0 {
		cfg.FailThreshold = DefaultFailThreshold
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultCheckTimeout
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultHeartbeatEndpoint
	}

	state := LoadState(cfg.StateFilePath)

	return &Engine{
		config:   cfg,
		checker:  checker,
		firewall: firewall,
		state:    state,
	}
}

// Evaluate executes one cycle of health probe and firewall rule reconciliation.
//
// Business rule: Opens port 22 if consecutive failures reach threshold; closes port 22 on recovery.
//
// @ai-constraint: Must persist state file and emergency log entries on every execution.
func (e *Engine) Evaluate(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	now := time.Now().UTC()
	e.state.LastCheckTime = now
	probeErr := e.checker.Check(checkCtx, e.config.Endpoint, e.config.AuthToken)

	if probeErr == nil {
		e.handleSuccess(now)
	} else {
		e.handleFailure(now, probeErr)
	}

	_, saveErr := SaveState(e.config.StateFilePath, e.state)
	return saveErr
}

// handleSuccess processes a healthy heartbeat probe.
func (e *Engine) handleSuccess(now time.Time) {
	if e.state.BreakGlassActive {
		e.logMessage("INFO", "[BREAK-GLASS] Control plane connection restored. Revoking emergency SSH port 22 rule.")
		if !e.config.DryRun {
			_ = e.firewall.DisableBreakGlass()
		}
		e.state.BreakGlassActive = false
	}

	e.state.ConsecutiveFailures = 0
	e.state.LastSuccessTime = now
	e.state.LastError = ""
	e.logMessage("INFO", fmt.Sprintf("[LIVENESS] Heartbeat probe successful (%s)", e.config.Endpoint))
}

// handleFailure processes an unhealthy heartbeat probe.
func (e *Engine) handleFailure(now time.Time, probeErr error) {
	e.state.ConsecutiveFailures++
	e.state.LastFailureTime = now
	e.state.LastError = probeErr.Error()

	e.logMessage("WARN", fmt.Sprintf("[LIVENESS] Heartbeat probe failed (%d/%d): %v",
		e.state.ConsecutiveFailures, e.config.FailThreshold, probeErr))

	if e.state.ConsecutiveFailures >= e.config.FailThreshold && !e.state.BreakGlassActive {
		alertMsg := fmt.Sprintf("[BREAK-GLASS] Consecutive failures reached %d. Enabling emergency rate-limited SSH port 22 rule.",
			e.state.ConsecutiveFailures)
		e.logMessage("ALERT", alertMsg)

		if !e.config.DryRun {
			_ = e.firewall.EnableBreakGlass()
		}
		e.state.BreakGlassActive = true
	}
}

// logMessage writes structured timestamped entries to system log and stderr.
func (e *Engine) logMessage(level, message string) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, strings.TrimSpace(message))

	fmt.Print(entry)

	logPath := e.config.LogFilePath
	if logPath == "" {
		logPath = DefaultLogFilePath
	}

	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, StateDirectoryPermission); err != nil {
		logPath = DefaultFallbackLogFilePath
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, EmergencyLogPermission)
	if err != nil && logPath != DefaultFallbackLogFilePath {
		logPath = DefaultFallbackLogFilePath
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, EmergencyLogPermission)
	}

	if err == nil {
		_, _ = logFile.WriteString(entry)
		_ = logFile.Close()
	}
}
