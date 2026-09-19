// Package tunnel verifies tunnel orchestration and fault tolerance under simulated connection failures.
package tunnel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockTunnelClient simulates low-level tunnel transport behaviour for test assertions.
type mockTunnelClient struct {
	startErr  error
	waitErr   error
	waitBlock chan struct{}
	closeErr  error
	mutex     sync.Mutex
	isClosed  bool
}

func newMockClient(startErr, waitErr error) *mockTunnelClient {
	return &mockTunnelClient{
		startErr:  startErr,
		waitErr:   waitErr,
		waitBlock: make(chan struct{}),
	}
}

func (m *mockTunnelClient) Start(_ context.Context) error {
	return m.startErr
}

func (m *mockTunnelClient) Wait() error {
	<-m.waitBlock
	return m.waitErr
}

func (m *mockTunnelClient) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if !m.isClosed {
		m.isClosed = true
		close(m.waitBlock)
	}
	return m.closeErr
}

// TestManagerLifecycle verifies normal startup, connected state transition, and clean shutdown.
func TestManagerLifecycle(t *testing.T) {
	mockClient := newMockClient(nil, nil)
	mockFactory := func(_ context.Context, _ Config) (Client, error) {
		return mockClient, nil
	}

	cfg := DefaultConfig()
	cfg.Server = "https://hub.example.com:443"
	cfg.AuthToken = "secret-token-xyz"
	cfg.BaseBackoff = 10 * time.Millisecond
	cfg.MaxBackoff = 50 * time.Millisecond

	mgr, err := NewManager(cfg, mockFactory)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	// Poll until connected state is verified
	var status TunnelStatus
	for attempt := 0; attempt < 20; attempt++ {
		status = mgr.Status()
		if status.Connected && status.State == StateConnected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !status.Connected {
		t.Fatalf("expected tunnel status Connected, got %+v", status)
	}

	if err := mgr.Stop(); err != nil {
		t.Fatalf("expected clean stop, got %v", err)
	}

	finalStatus := mgr.Status()
	if finalStatus.State != StateClosed {
		t.Fatalf("expected state %s after stop, got %s", StateClosed, finalStatus.State)
	}
}

// TestManagerReconnectOnFailure verifies backoff loop increments reconnect count on transport error.
func TestManagerReconnectOnFailure(t *testing.T) {
	dialFailures := 0
	simulatedErr := errors.New("simulated dial timeout")

	mockFactory := func(_ context.Context, _ Config) (Client, error) {
		dialFailures++
		if dialFailures <= 2 {
			return nil, simulatedErr
		}
		return newMockClient(nil, nil), nil
	}

	cfg := DefaultConfig()
	cfg.Server = "https://hub.example.com:443"
	cfg.AuthToken = "token"
	cfg.BaseBackoff = 10 * time.Millisecond
	cfg.MaxBackoff = 30 * time.Millisecond

	mgr, err := NewManager(cfg, mockFactory)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer func() { _ = mgr.Stop() }()

	// Wait for successful recovery after initial dial failures
	for attempt := 0; attempt < 30; attempt++ {
		status := mgr.Status()
		if status.Connected && status.ReconnectCount >= 2 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}

	status := mgr.Status()
	if status.ReconnectCount < 2 {
		t.Fatalf("expected at least 2 reconnect attempts, got %d", status.ReconnectCount)
	}
}

// TestConfigValidation verifies input guardrails.
func TestConfigValidation(t *testing.T) {
	invalidCfg := Config{}
	if err := invalidCfg.Validate(); err == nil {
		t.Fatal("expected error on empty config, got nil")
	}

	validCfg := DefaultConfig()
	validCfg.Server = "hub.example.com:443"
	validCfg.AuthToken = "tok"
	if err := validCfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	if validCfg.Server != "https://hub.example.com:443" {
		t.Fatalf("expected auto-prepended https://, got %s", validCfg.Server)
	}
}

// TestStatusTrackerJSON verifies serialization of telemetry metrics.
func TestStatusTrackerJSON(t *testing.T) {
	tracker := NewStatusTracker()
	tracker.RecordConnected("https://hub.example.com:443", "127.0.0.1:8090")
	tracker.RecordHeartbeat(15 * time.Millisecond)

	jsonBytes, err := tracker.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize status to JSON: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Fatal("expected non-empty JSON output")
	}
}
