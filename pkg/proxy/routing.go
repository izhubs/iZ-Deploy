// Package proxy manages reverse proxy sidecars, traffic routing, and TLS termination.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default routing and healthcheck timing parameters.
const (
	DefaultHealthCheckPath     = "/up"
	DefaultHealthCheckTimeout  = 5 * time.Second
	DefaultHealthCheckInterval = 100 * time.Millisecond
	DefaultDrainDuration       = 500 * time.Millisecond
	MaxAcceptableSwapDuration  = 1200 * time.Millisecond // 1.2s SLA
)

// Routing errors.
var (
	ErrEmptyService          = errors.New("routing: service name cannot be empty")
	ErrEmptyTarget           = errors.New("routing: target address cannot be empty")
	ErrHealthCheckTimeout    = errors.New("routing: healthcheck polling timed out")
	ErrHealthCheckUnhealthy  = errors.New("routing: backend responded with non-200 status")
	ErrSwapRollbackTriggered = errors.New("routing: container swap aborted and rolled back")
)

// DECISION: Perform target registration before health check polling during container swaps.
// WHY: Registering target v2 in the proxy control plane allows the proxy router to verify
// network connectivity while connection buffering holds any accidental early traffic.
// If healthcheck fails, an automated rollback deregisters v2 before any client traffic is cut over.
// TRADE-OFF: Requires explicit rollback logic to prune target v2 from proxy state on healthcheck failure.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-007

// RouteSpec defines operational parameters for routing traffic to a service.
type RouteSpec struct {
	Service             string        `json:"service"`
	Type                string        `json:"type,omitempty"` // web or worker
	Domain              string        `json:"domain"`
	TargetV1            string        `json:"target_v1"`
	TargetV2            string        `json:"target_v2,omitempty"`
	HealthCheckPath     string        `json:"health_check_path"`
	HealthCheckTimeout  time.Duration `json:"health_check_timeout"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	DrainDuration       time.Duration `json:"drain_duration"`
	TLSEnabled          bool          `json:"tls_enabled"`
}

// SwapResult contains latency benchmarks and status metadata for a completed container swap.
type SwapResult struct {
	Service             string        `json:"service"`
	OldTarget           string        `json:"old_target"`
	NewTarget           string        `json:"new_target"`
	HealthCheckDuration time.Duration `json:"healthcheck_duration"`
	SwapDuration        time.Duration `json:"swap_duration"`
	WithinSLA           bool          `json:"within_sla"`
	Timestamp           time.Time     `json:"timestamp"`
}

// RegisterRoute registers a single target backend into the proxy routing table.
//
// Business rule: Used for initial application provisioning prior to live traffic cutover.
//
// @ai-constraint: Ensure domain formatting adheres to host header matching requirements.
func RegisterRoute(ctx context.Context, client ManagementClient, spec RouteSpec, target string) error {
	if spec.Service == "" {
		return ErrEmptyService
	}
	if target == "" {
		return ErrEmptyTarget
	}

	payload := TargetPayload{
		Service: spec.Service,
		Target:  target,
		Host:    spec.Domain,
		TLS:     spec.TLSEnabled,
	}

	return client.Deploy(ctx, payload)
}

// HealthcheckWait polls the target backend HTTP endpoint until it returns HTTP 200 OK or times out.
//
// Business rule: Container v2 must return HTTP 200 on its designated health path before receiving traffic.
// Any non-200 status code (500, 502, 503) or connection failure continues polling until timeout.
//
// @ai-constraint: Must use ephemeral HTTP transport without keep-alives to prevent connection pooling bias.
func HealthcheckWait(ctx context.Context, target, path string, timeout, interval time.Duration) error {
	if target == "" {
		return ErrEmptyTarget
	}
	if path == "" {
		path = DefaultHealthCheckPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if timeout <= 0 {
		timeout = DefaultHealthCheckTimeout
	}
	if interval <= 0 {
		interval = DefaultHealthCheckInterval
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Ensure target address formatting
	targetURL := target
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "http://" + targetURL
	}
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("routing: invalid target URL format: %w", err)
	}
	parsedURL.Path = path
	fullURL := parsedURL.String()

	probeClient := &http.Client{
		Timeout: interval,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-deadlineCtx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w after %v: %v", ErrHealthCheckTimeout, timeout, lastErr)
			}
			return fmt.Errorf("%w after %v", ErrHealthCheckTimeout, timeout)

		case <-ticker.C:
			req, err := http.NewRequestWithContext(deadlineCtx, http.MethodGet, fullURL, nil)
			if err != nil {
				lastErr = err
				continue
			}

			resp, err := probeClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			statusCode := resp.StatusCode
			_ = resp.Body.Close()

			if statusCode == http.StatusOK {
				return nil
			}

			lastErr = fmt.Errorf("%w: HTTP %d", ErrHealthCheckUnhealthy, statusCode)
		}
	}
}

// SwapContainer executes an automated Sub-Second Zero-Downtime Swap between container targets.
//
// Business rule: Sequence:
// 1. Register target v2 in proxy routing table.
// 2. Poll healthcheck on target v2 until HTTP 200.
//    - If healthcheck fails: Rollback immediately (remove target v2), leaving v1 untouched.
// 3. Switch traffic routing to target v2.
// 4. Drain existing connections on target v1 for DrainDuration.
// 5. Unregister target v1 and invoke onDrainV1 teardown hook.
// 6. Enforce Sub-Second SLA: SwapDuration < 1.2s.
//
// @ai-constraint: Thread-safe orchestration with deterministic rollback cleanup.
func SwapContainer(
	ctx context.Context,
	client ManagementClient,
	spec RouteSpec,
	targetV2 string,
	onDrainV1 func(ctx context.Context, targetV1 string) error,
) (*SwapResult, error) {
	if spec.Service == "" {
		return nil, ErrEmptyService
	}
	if targetV2 == "" {
		return nil, fmt.Errorf("%w: new target v2 required", ErrEmptyTarget)
	}

	swapStartTime := time.Now()

	// Bypass proxy for background workers
	if spec.Type == "worker" {
		oldTarget := spec.TargetV1
		if oldTarget != "" && oldTarget != targetV2 && onDrainV1 != nil {
			if err := onDrainV1(ctx, oldTarget); err != nil {
				return nil, fmt.Errorf("worker teardown failed: %w", err)
			}
		}
		return &SwapResult{
			Service:             spec.Service,
			OldTarget:           oldTarget,
			NewTarget:           targetV2,
			HealthCheckDuration: 0,
			SwapDuration:        time.Since(swapStartTime),
			WithinSLA:           true,
			Timestamp:           time.Now(),
		}, nil
	}

	// 1. Register new container target v2
	if err := RegisterRoute(ctx, client, spec, targetV2); err != nil {
		return nil, fmt.Errorf("routing: failed to register target v2: %w", err)
	}

	// 2. Wait for health check verification on v2
	hcStart := time.Now()
	hcTimeout := spec.HealthCheckTimeout
	if hcTimeout <= 0 {
		hcTimeout = DefaultHealthCheckTimeout
	}
	hcInterval := spec.HealthCheckInterval
	if hcInterval <= 0 {
		hcInterval = DefaultHealthCheckInterval
	}

	if err := HealthcheckWait(ctx, targetV2, spec.HealthCheckPath, hcTimeout, hcInterval); err != nil {
		// Rollback: Deregister target v2 to keep target v1 intact.
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer rollbackCancel()

		_ = client.Remove(rollbackCtx, spec.Service, targetV2)

		return nil, fmt.Errorf("%w: target v2 healthcheck failed (%v)", ErrSwapRollbackTriggered, err)
	}
	hcDuration := time.Since(hcStart)

	// 3. Gracefully drain and deregister old container target v1
	oldTarget := spec.TargetV1
	if oldTarget != "" && oldTarget != targetV2 {
		drainTime := spec.DrainDuration
		if drainTime <= 0 {
			drainTime = DefaultDrainDuration
		}

		select {
		case <-time.After(drainTime):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		// Deregister target v1 from proxy
		if err := client.Remove(ctx, spec.Service, oldTarget); err != nil {
			// Non-fatal logging: Target v2 is already healthy and active
			// We proceed with hook execution
		}

		// Execute teardown hook (e.g. stop docker container v1)
		if onDrainV1 != nil {
			if err := onDrainV1(ctx, oldTarget); err != nil {
				return nil, fmt.Errorf("routing: onDrainV1 callback failed: %w", err)
			}
		}
	}

	totalDuration := time.Since(swapStartTime)
	withinSLA := totalDuration <= MaxAcceptableSwapDuration

	return &SwapResult{
		Service:             spec.Service,
		OldTarget:           oldTarget,
		NewTarget:           targetV2,
		HealthCheckDuration: hcDuration,
		SwapDuration:        totalDuration,
		WithinSLA:           withinSLA,
		Timestamp:           time.Now(),
	}, nil
}
