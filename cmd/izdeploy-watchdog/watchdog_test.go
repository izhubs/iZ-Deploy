// Package main verifies watchdog failure counting, break-glass threshold triggers, and firewall recovery.
package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// mockChecker simulates liveness probe responses for unit tests.
type mockChecker struct {
	shouldFail bool
	failErr    error
}

func (m *mockChecker) Check(_ context.Context, _, _ string) error {
	if m.shouldFail {
		if m.failErr != nil {
			return m.failErr
		}
		return errors.New("simulated probe network error")
	}
	return nil
}

// TestEvaluateSuccess verifies successful heartbeat resets failures and maintains closed port.
func TestEvaluateSuccess(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "watchdog.state")
	logFile := filepath.Join(tempDir, "watchdog.log")

	cfg := EngineConfig{
		Endpoint:      "http://127.0.0.1:8090/healthz",
		FailThreshold: 5,
		StateFilePath: stateFile,
		LogFilePath:   logFile,
		DryRun:        true,
		Timeout:       time.Second,
	}

	checker := &mockChecker{shouldFail: false}
	firewall := &MockFirewall{}
	engine := NewEngine(cfg, checker, firewall)

	if err := engine.Evaluate(context.Background()); err != nil {
		t.Fatalf("evaluate returned error: %v", err)
	}

	if engine.state.ConsecutiveFailures != 0 {
		t.Fatalf("expected 0 failures, got %d", engine.state.ConsecutiveFailures)
	}

	if engine.state.BreakGlassActive {
		t.Fatal("expected break-glass to remain inactive")
	}

	if firewall.enableCount != 0 {
		t.Fatalf("expected 0 firewall enables, got %d", firewall.enableCount)
	}
}

// TestEvaluateFailureIncrementsCounter verifies failure counts accumulate without prematurely triggering firewall.
func TestEvaluateFailureIncrementsCounter(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "watchdog.state")
	logFile := filepath.Join(tempDir, "watchdog.log")

	cfg := EngineConfig{
		Endpoint:      "http://127.0.0.1:8090/healthz",
		FailThreshold: 3,
		StateFilePath: stateFile,
		LogFilePath:   logFile,
		DryRun:        true,
		Timeout:       time.Second,
	}

	checker := &mockChecker{shouldFail: true}
	firewall := &MockFirewall{}
	engine := NewEngine(cfg, checker, firewall)

	for cycle := 1; cycle <= 2; cycle++ {
		if err := engine.Evaluate(context.Background()); err != nil {
			t.Fatalf("cycle %d evaluate returned error: %v", cycle, err)
		}
		if engine.state.ConsecutiveFailures != cycle {
			t.Fatalf("expected %d failures, got %d", cycle, engine.state.ConsecutiveFailures)
		}
		if engine.state.BreakGlassActive {
			t.Fatalf("break-glass activated prematurely at cycle %d", cycle)
		}
		if firewall.enableCount != 0 {
			t.Fatalf("firewall enabled prematurely at cycle %d", cycle)
		}
	}
}

// TestEvaluateBreakGlassTriggerAtThreshold verifies firewall rule is injected once threshold is breached.
func TestEvaluateBreakGlassTriggerAtThreshold(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "watchdog.state")
	logFile := filepath.Join(tempDir, "watchdog.log")

	cfg := EngineConfig{
		Endpoint:      "http://127.0.0.1:8090/healthz",
		FailThreshold: 3,
		StateFilePath: stateFile,
		LogFilePath:   logFile,
		DryRun:        false,
		Timeout:       time.Second,
	}

	checker := &mockChecker{shouldFail: true}
	firewall := &MockFirewall{}
	engine := NewEngine(cfg, checker, firewall)

	// Simulate 3 consecutive failures
	for cycle := 1; cycle <= 3; cycle++ {
		if err := engine.Evaluate(context.Background()); err != nil {
			t.Fatalf("cycle %d evaluate returned error: %v", cycle, err)
		}
	}

	if !engine.state.BreakGlassActive {
		t.Fatal("expected break-glass to be active after reaching threshold")
	}

	if firewall.enableCount != 1 {
		t.Fatalf("expected exactly 1 firewall enable invocation, got %d", firewall.enableCount)
	}

	// 4th failure must NOT duplicate enable call (idempotent)
	if err := engine.Evaluate(context.Background()); err != nil {
		t.Fatalf("cycle 4 evaluate returned error: %v", err)
	}

	if firewall.enableCount != 1 {
		t.Fatalf("expected firewall enableCount to remain 1, got %d", firewall.enableCount)
	}
}

// TestEvaluateRecoveryClosesPort22 verifies successful probe immediately revokes break-glass rule.
func TestEvaluateRecoveryClosesPort22(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "watchdog.state")
	logFile := filepath.Join(tempDir, "watchdog.log")

	cfg := EngineConfig{
		Endpoint:      "http://127.0.0.1:8090/healthz",
		FailThreshold: 2,
		StateFilePath: stateFile,
		LogFilePath:   logFile,
		DryRun:        false,
		Timeout:       time.Second,
	}

	checker := &mockChecker{shouldFail: true}
	firewall := &MockFirewall{}
	engine := NewEngine(cfg, checker, firewall)

	// Trigger break-glass
	_ = engine.Evaluate(context.Background())
	_ = engine.Evaluate(context.Background())

	if !engine.state.BreakGlassActive {
		t.Fatal("expected break-glass to be active")
	}

	// Connection recovers
	checker.shouldFail = false
	if err := engine.Evaluate(context.Background()); err != nil {
		t.Fatalf("recovery evaluate returned error: %v", err)
	}

	if engine.state.BreakGlassActive {
		t.Fatal("expected break-glass to be deactivated after recovery")
	}

	if engine.state.ConsecutiveFailures != 0 {
		t.Fatalf("expected failure count reset to 0, got %d", engine.state.ConsecutiveFailures)
	}

	if firewall.deleteCount != 1 {
		t.Fatalf("expected exactly 1 firewall delete invocation, got %d", firewall.deleteCount)
	}
}

// TestStatePersistence verifies state survives across distinct engine instances.
func TestStatePersistence(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "watchdog.state")
	logFile := filepath.Join(tempDir, "watchdog.log")

	initialState := &State{
		ConsecutiveFailures: 7,
		BreakGlassActive:    false,
		LastError:           "connection timeout",
	}

	if _, err := SaveState(stateFile, initialState); err != nil {
		t.Fatalf("failed to save initial state: %v", err)
	}

	loaded := LoadState(stateFile)
	if loaded.ConsecutiveFailures != 7 {
		t.Fatalf("expected 7 failures loaded, got %d", loaded.ConsecutiveFailures)
	}

	cfg := EngineConfig{
		Endpoint:      "http://127.0.0.1:8090/healthz",
		FailThreshold: 10,
		StateFilePath: stateFile,
		LogFilePath:   logFile,
		DryRun:        true,
	}

	checker := &mockChecker{shouldFail: true}
	firewall := &MockFirewall{}
	engine := NewEngine(cfg, checker, firewall)

	if engine.state.ConsecutiveFailures != 7 {
		t.Fatalf("expected engine to inherit 7 failures, got %d", engine.state.ConsecutiveFailures)
	}

	_ = engine.Evaluate(context.Background())
	if engine.state.ConsecutiveFailures != 8 {
		t.Fatalf("expected failure count incremented to 8, got %d", engine.state.ConsecutiveFailures)
	}
}
