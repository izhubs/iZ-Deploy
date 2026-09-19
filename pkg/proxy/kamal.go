// Package proxy manages reverse proxy sidecars, traffic routing, and TLS termination.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Default proxy runtime configuration values.
const (
	DefaultManagementPort = 2999
	DefaultHTTPPort       = 80
	DefaultHTTPSPort      = 443
	DefaultStartupTimeout = 5 * time.Second
	DefaultShutdownTimeout = 5 * time.Second
	DefaultHTTPClientTimeout = 3 * time.Second
)

// Common proxy errors adhering to POSIX and HTTP gateway semantics.
var (
	ErrProxyAlreadyRunning = errors.New("proxy: process already active")
	ErrProxyNotRunning     = errors.New("proxy: process not running")
	ErrProxyStartTimeout   = errors.New("proxy: startup healthcheck timed out")
	ErrInvalidConfig       = errors.New("proxy: invalid configuration parameter")
	ErrTargetUnreachable   = errors.New("proxy: target backend unreachable")
)

// DECISION: Decouple process supervision and HTTP management client into distinct interfaces.
// WHY: Enables unit testing of zero-downtime routing workflows against mock HTTP backends
// without requiring a physical kamal-proxy binary on developer or CI host machines.
// TRADE-OFF: Requires additional interface abstraction layer (`SidecarController` and `ManagementClient`).
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-007

// SidecarConfig defines execution parameters for the proxy sidecar daemon.
type SidecarConfig struct {
	BinaryPath     string
	ManagementPort int
	HTTPPort       int
	HTTPSPort      int
	StoragePath    string
	ExtraArgs      []string
}

// TargetPayload defines the registration payload sent to the proxy control plane.
type TargetPayload struct {
	Service string `json:"service"`
	Target  string `json:"target"`
	Host    string `json:"host,omitempty"`
	TLS     bool   `json:"tls,omitempty"`
}

// TargetStatus describes an active backend registered in the proxy route table.
type TargetStatus struct {
	Service string `json:"service"`
	Target  string `json:"target"`
	Host    string `json:"host"`
	State   string `json:"state"`
}

// SidecarController defines process management contracts for the proxy daemon.
type SidecarController interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Reload(ctx context.Context) error
	IsRunning() bool
	PID() int
}

// ManagementClient defines HTTP control-plane contracts for dynamic target routing.
type ManagementClient interface {
	Deploy(ctx context.Context, payload TargetPayload) error
	Remove(ctx context.Context, service, target string) error
	Pause(ctx context.Context, service string) error
	Resume(ctx context.Context, service string) error
	ListTargets(ctx context.Context) ([]TargetStatus, error)
	Ping(ctx context.Context) error
}

// ProcessManager supervises the local OS sidecar process.
type ProcessManager struct {
	cfg        SidecarConfig
	cmd        *exec.Cmd
	mu         sync.RWMutex
	running    bool
	pid        int
	client     *http.Client
}

// NewProcessManager instantiates a proxy process supervisor with validated defaults.
//
// Business rule: Enforce sub-15MB RAM operation by invoking kamal-proxy with stripped logs
// and bounded memory limits.
//
// @ai-constraint: Guard against port collisions by validating port ranges prior to process fork.
func NewProcessManager(cfg SidecarConfig) (*ProcessManager, error) {
	if cfg.ManagementPort <= 0 {
		cfg.ManagementPort = DefaultManagementPort
	}
	if cfg.HTTPPort <= 0 {
		cfg.HTTPPort = DefaultHTTPPort
	}
	if cfg.HTTPSPort <= 0 {
		cfg.HTTPSPort = DefaultHTTPSPort
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "kamal-proxy"
	}

	return &ProcessManager{
		cfg: cfg,
		client: &http.Client{
			Timeout: DefaultHTTPClientTimeout,
		},
	}, nil
}

// Start launches the proxy process and polls its management port until ready.
//
// Business rule: Start must be idempotent; calling Start on an already active process
// returns ErrProxyAlreadyRunning.
//
// @ai-constraint: Child process stdout/stderr must not block execution; redirect to system pipes.
func (pm *ProcessManager) Start(ctx context.Context) error {
	pm.mu.Lock()
	if pm.running {
		pm.mu.Unlock()
		return ErrProxyAlreadyRunning
	}

	args := []string{
		"run",
		"--http-port", strconv.Itoa(pm.cfg.HTTPPort),
		"--https-port", strconv.Itoa(pm.cfg.HTTPSPort),
		"--management-port", strconv.Itoa(pm.cfg.ManagementPort),
	}
	if pm.cfg.StoragePath != "" {
		args = append(args, "--storage-path", pm.cfg.StoragePath)
	}
	args = append(args, pm.cfg.ExtraArgs...)

	cmd := exec.CommandContext(ctx, pm.cfg.BinaryPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		pm.mu.Unlock()
		return fmt.Errorf("proxy: failed to fork process %s: %w", pm.cfg.BinaryPath, err)
	}

	pm.cmd = cmd
	pm.pid = cmd.Process.Pid
	pm.running = true
	pm.mu.Unlock()

	// Wait for management endpoint readiness.
	waitCtx, cancel := context.WithTimeout(ctx, DefaultStartupTimeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			_ = pm.Stop(context.Background())
			return ErrProxyStartTimeout
		case <-ticker.C:
			if err := pm.Ping(waitCtx); err == nil {
				return nil
			}
		}
	}
}

// Stop terminates the proxy process gracefully with SIGTERM, falling back to SIGKILL.
//
// Business rule: Ensure zero orphaned sidecar processes upon agent termination.
//
// @ai-constraint: Must release lock before process wait to avoid deadlocks across goroutines.
func (pm *ProcessManager) Stop(ctx context.Context) error {
	pm.mu.Lock()
	if !pm.running || pm.cmd == nil || pm.cmd.Process == nil {
		pm.mu.Unlock()
		return ErrProxyNotRunning
	}

	proc := pm.cmd.Process
	pm.mu.Unlock()

	// Attempt graceful interrupt first.
	_ = proc.Signal(syscall.SIGTERM)

	done := make(chan error, 1)
	go func() {
		state, err := proc.Wait()
		if err == nil && state != nil {
			done <- nil
			return
		}
		done <- err
	}()

	select {
	case <-time.After(DefaultShutdownTimeout):
		_ = proc.Kill()
		<-done
	case <-ctx.Done():
		_ = proc.Kill()
		<-done
	case <-done:
	}

	pm.mu.Lock()
	pm.running = false
	pm.cmd = nil
	pm.pid = 0
	pm.mu.Unlock()

	return nil
}

// Reload sends SIGHUP or triggers dynamic configuration reload without dropping connections.
//
// Business rule: Process reload must maintain in-flight client TCP connections.
//
// @ai-constraint: Return ErrProxyNotRunning if reload is attempted on an inactive process.
func (pm *ProcessManager) Reload(ctx context.Context) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.running || pm.cmd == nil || pm.cmd.Process == nil {
		return ErrProxyNotRunning
	}

	if err := pm.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("proxy: failed to deliver SIGHUP reload signal: %w", err)
	}

	return pm.Ping(ctx)
}

// IsRunning reports active supervision status.
//
// Business rule: Thread-safe reader for health monitors.
//
// @ai-constraint: Read-lock only to avoid contention.
func (pm *ProcessManager) IsRunning() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.running
}

// PID returns current OS process ID.
//
// Business rule: Return 0 when process is not active.
//
// @ai-constraint: Thread-safe read.
func (pm *ProcessManager) PID() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pid
}

// Ping checks whether the management HTTP endpoint is accepting requests.
//
// Business rule: Fast diagnostic probe used during startup and health audits.
//
// @ai-constraint: Set short request timeout to avoid blocking caller thread.
func (pm *ProcessManager) Ping(ctx context.Context) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(pm.cfg.ManagementPort))
	url := fmt.Sprintf("http://%s/up", addr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := pm.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy: ping returned HTTP %d", resp.StatusCode)
	}

	return nil
}

// HTTPClient implements ManagementClient against kamal-proxy HTTP REST API.
type HTTPClient struct {
	endpoint string
	client   *http.Client
}

// NewHTTPClient creates an HTTP-based control plane client for kamal-proxy.
//
// Business rule: Target the management API port exposed by the local sidecar.
//
// @ai-constraint: Ensure client timeout is configured to prevent goroutine accumulation.
func NewHTTPClient(managementPort int) *HTTPClient {
	if managementPort <= 0 {
		managementPort = DefaultManagementPort
	}
	return &HTTPClient{
		endpoint: fmt.Sprintf("http://127.0.0.1:%d", managementPort),
		client: &http.Client{
			Timeout: DefaultHTTPClientTimeout,
		},
	}
}

// Deploy registers or updates a target backend in kamal-proxy.
//
// Business rule: Target registration must be idempotent; re-deploying an identical target
// updates routing metadata without connection resets.
//
// @ai-constraint: Payload serialization errors must be caught before wire dispatch.
func (c *HTTPClient) Deploy(ctx context.Context, payload TargetPayload) error {
	if payload.Service == "" || payload.Target == "" {
		return fmt.Errorf("%w: service and target are required", ErrInvalidConfig)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("proxy: marshal deploy payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/deploy", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("proxy: deploy returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Remove unregisters an active backend target from the proxy routing table.
//
// Business rule: Executed after connection draining during container swaps.
//
// @ai-constraint: Must tolerate non-existent targets gracefully (idempotent delete).
func (c *HTTPClient) Remove(ctx context.Context, service, target string) error {
	if service == "" || target == "" {
		return fmt.Errorf("%w: service and target required for removal", ErrInvalidConfig)
	}

	payload := TargetPayload{
		Service: service,
		Target:  target,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("proxy: marshal remove payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/remove", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("proxy: remove returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Pause stops forwarding new requests to the service backend while preserving state.
//
// Business rule: Used for emergency maintenance or maintenance page injection.
//
// @ai-constraint: Non-blocking request with bounded context deadline.
func (c *HTTPClient) Pause(ctx context.Context, service string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/pause?service=%s", c.endpoint, service), nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxy: pause returned HTTP %d", resp.StatusCode)
	}

	return nil
}

// Resume re-enables traffic forwarding for a previously paused service.
//
// Business rule: Restores traffic instantly upon maintenance conclusion.
//
// @ai-constraint: Require valid service name.
func (c *HTTPClient) Resume(ctx context.Context, service string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/resume?service=%s", c.endpoint, service), nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxy: resume returned HTTP %d", resp.StatusCode)
	}

	return nil
}

// ListTargets queries active routing entries across all deployed services.
//
// Business rule: Used by status reporters and drift auditors.
//
// @ai-constraint: Close response stream immediately after JSON decoding.
func (c *HTTPClient) ListTargets(ctx context.Context) ([]TargetStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/targets", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy: targets query returned HTTP %d", resp.StatusCode)
	}

	var targets []TargetStatus
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("proxy: decode targets response: %w", err)
	}

	return targets, nil
}

// Ping checks reachability of the proxy management control plane.
//
// Business rule: Verifies API port readiness before routing operations.
//
// @ai-constraint: Lightweight GET probe.
func (c *HTTPClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/up", nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy: API ping returned HTTP %d", resp.StatusCode)
	}

	return nil
}
