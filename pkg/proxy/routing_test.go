// Package proxy verifies zero-downtime routing, target swapping, and healthcheck polling.
package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockManagementClient tracks deployment and removal actions for testing.
type mockManagementClient struct {
	mu           sync.Mutex
	deployed     []TargetPayload
	removed      []string
	deployErr    error
	removeErr    error
	listTargets  []TargetStatus
	pingErr      error
}

func (m *mockManagementClient) Deploy(ctx context.Context, payload TargetPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deployErr != nil {
		return m.deployErr
	}
	m.deployed = append(m.deployed, payload)
	return nil
}

func (m *mockManagementClient) Remove(ctx context.Context, service, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.removeErr != nil {
		return m.removeErr
	}
	m.removed = append(m.removed, target)
	return nil
}

func (m *mockManagementClient) Pause(ctx context.Context, service string) error {
	return nil
}

func (m *mockManagementClient) Resume(ctx context.Context, service string) error {
	return nil
}

func (m *mockManagementClient) ListTargets(ctx context.Context) ([]TargetStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listTargets, nil
}

func (m *mockManagementClient) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pingErr
}

// TestRegisterRoute verifies route specifications are translated and submitted to the proxy client.
//
// Business rule: Target registration must fail fast on empty service or target fields.
//
// @ai-constraint: Verify payload parameter fidelity between RouteSpec and TargetPayload.
func TestRegisterRoute(t *testing.T) {
	client := &mockManagementClient{}
	ctx := context.Background()

	spec := RouteSpec{
		Service:    "app-api",
		Domain:     "api.example.com",
		TargetV1:   "127.0.0.1:8080",
		TLSEnabled: true,
	}

	err := RegisterRoute(ctx, client, spec, spec.TargetV1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(client.deployed) != 1 {
		t.Fatalf("expected 1 deployed target, got %d", len(client.deployed))
	}
	if client.deployed[0].Service != "app-api" || client.deployed[0].Target != "127.0.0.1:8080" {
		t.Errorf("deployed payload mismatch: %+v", client.deployed[0])
	}

	// Empty service check
	errEmptyService := RegisterRoute(ctx, client, RouteSpec{}, "127.0.0.1:8080")
	if !errors.Is(errEmptyService, ErrEmptyService) {
		t.Errorf("expected ErrEmptyService, got %v", errEmptyService)
	}

	// Empty target check
	errEmptyTarget := RegisterRoute(ctx, client, spec, "")
	if !errors.Is(errEmptyTarget, ErrEmptyTarget) {
		t.Errorf("expected ErrEmptyTarget, got %v", errEmptyTarget)
	}
}

// TestHealthcheckWait_Success verifies polling succeeds when the backend returns HTTP 200.
//
// Business rule: Healthcheck probe must accept initial failures and succeed upon reaching HTTP 200.
//
// @ai-constraint: Use local httptest server with minimal polling intervals to prevent slow test suites.
func TestHealthcheckWait_Success(t *testing.T) {
	var attempts int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			http.Error(w, "warming up", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	ctx := context.Background()
	err := HealthcheckWait(ctx, ts.Listener.Addr().String(), "/up", 2*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("expected HealthcheckWait to succeed, got %v", err)
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts < 3 {
		t.Errorf("expected at least 3 polling attempts, got %d", finalAttempts)
	}
}

// TestHealthcheckWait_Timeout verifies that persistent non-200 responses return ErrHealthCheckTimeout.
//
// Business rule: If target backend does not recover within timeout, HealthcheckWait returns timeout error.
//
// @ai-constraint: Bounded short timeout (200ms) for fast unit test execution.
func TestHealthcheckWait_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	ctx := context.Background()
	err := HealthcheckWait(ctx, ts.Listener.Addr().String(), "/up", 200*time.Millisecond, 40*time.Millisecond)
	if err == nil {
		t.Fatal("expected healthcheck timeout error, got nil")
	}
	if !errors.Is(err, ErrHealthCheckTimeout) {
		t.Errorf("expected ErrHealthCheckTimeout, got %v", err)
	}
}

// TestSwapContainer_Success verifies end-to-end zero-downtime container swap.
//
// Business rule: Sequence: register v2 -> verify v2 health -> drain v1 -> deregister v1.
// Total swap duration must adhere to sub-1.2s SLA.
//
// @ai-constraint: Ensure mock teardown hook is called with old target.
func TestSwapContainer_Success(t *testing.T) {
	v2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer v2Server.Close()

	client := &mockManagementClient{}
	ctx := context.Background()

	spec := RouteSpec{
		Service:             "web-service",
		Domain:              "app.example.com",
		TargetV1:            "127.0.0.1:8001",
		HealthCheckPath:     "/up",
		HealthCheckTimeout:  1 * time.Second,
		HealthCheckInterval: 20 * time.Millisecond,
		DrainDuration:       50 * time.Millisecond,
		TLSEnabled:          true,
	}

	targetV2 := v2Server.Listener.Addr().String()
	var drainedTarget string
	teardownHook := func(ctx context.Context, target string) error {
		drainedTarget = target
		return nil
	}

	result, err := SwapContainer(ctx, client, spec, targetV2, teardownHook)
	if err != nil {
		t.Fatalf("expected SwapContainer to succeed, got %v", err)
	}

	if result.OldTarget != "127.0.0.1:8001" {
		t.Errorf("expected OldTarget 127.0.0.1:8001, got %s", result.OldTarget)
	}
	if result.NewTarget != targetV2 {
		t.Errorf("expected NewTarget %s, got %s", targetV2, result.NewTarget)
	}
	if !result.WithinSLA {
		t.Errorf("expected swap within SLA, swap duration was %v", result.SwapDuration)
	}
	if drainedTarget != "127.0.0.1:8001" {
		t.Errorf("expected teardown hook called with 127.0.0.1:8001, got %s", drainedTarget)
	}

	// Verify client calls
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deployed) != 1 || client.deployed[0].Target != targetV2 {
		t.Errorf("expected deploy called for targetV2, got %+v", client.deployed)
	}
	if len(client.removed) != 1 || client.removed[0] != "127.0.0.1:8001" {
		t.Errorf("expected remove called for targetV1, got %+v", client.removed)
	}
}

// TestSwapContainer_HealthcheckFailureRollback verifies that healthcheck failure triggers rollback.
//
// Business rule: When container v2 fails health check, swap must abort, deregister v2,
// and preserve target v1 without calling the drain/teardown hook.
//
// @ai-constraint: Ensure target v1 is never removed on rollback.
func TestSwapContainer_HealthcheckFailureRollback(t *testing.T) {
	// v2 returns 500 error permanently
	v2Unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "crash", http.StatusInternalServerError)
	}))
	defer v2Unhealthy.Close()

	client := &mockManagementClient{}
	ctx := context.Background()

	spec := RouteSpec{
		Service:             "api-service",
		Domain:              "api.example.com",
		TargetV1:            "127.0.0.1:9001",
		HealthCheckPath:     "/up",
		HealthCheckTimeout:  100 * time.Millisecond,
		HealthCheckInterval: 20 * time.Millisecond,
		DrainDuration:       50 * time.Millisecond,
	}

	targetV2 := v2Unhealthy.Listener.Addr().String()
	hookCalled := false
	teardownHook := func(ctx context.Context, target string) error {
		hookCalled = true
		return nil
	}

	_, err := SwapContainer(ctx, client, spec, targetV2, teardownHook)
	if err == nil {
		t.Fatal("expected swap failure error, got nil")
	}
	if !errors.Is(err, ErrSwapRollbackTriggered) {
		t.Errorf("expected ErrSwapRollbackTriggered, got %v", err)
	}

	if hookCalled {
		t.Error("teardown hook should not be called when rollback occurs")
	}

	// Verify rollback removed targetV2 and did NOT remove targetV1
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.removed) != 1 || client.removed[0] != targetV2 {
		t.Errorf("expected rollback to remove targetV2 %s, got removed list: %+v", targetV2, client.removed)
	}
}
