package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestStripANSI(t *testing.T) {
	colored := "\x1b[31;1mERROR:\x1b[0m Failed to bind port \x1b[33m3000\x1b[0m"
	stripped := StripANSI(colored)
	expected := "ERROR: Failed to bind port 3000"

	if stripped != expected {
		t.Errorf("StripANSI() = %q, want %q", stripped, expected)
	}
}

func TestRingBufferLogs(t *testing.T) {
	// Case 1: Small log (< 60 lines)
	smallLogs := []string{"line 1", "line 2", "line 3"}
	resultSmall := RingBufferLogs(smallLogs, 15, 45)
	if resultSmall != "line 1\nline 2\nline 3" {
		t.Errorf("unexpected output for small logs: %q", resultSmall)
	}

	// Case 2: Large log (100 lines)
	var largeLogs []string
	for i := 1; i <= 100; i++ {
		largeLogs = append(largeLogs, "\x1b[32mline\x1b[0m "+string(rune('A'+(i%26))))
	}
	resultLarge := RingBufferLogs(largeLogs, 15, 45)

	lines := strings.Split(resultLarge, "\n")
	// 15 head + 1 truncation marker + 45 tail = 61 lines
	if len(lines) != 61 {
		t.Errorf("expected 61 lines in output, got %d", len(lines))
	}

	if !strings.Contains(resultLarge, "--- [truncated 40 lines] ---") {
		t.Errorf("expected truncation notice for 40 lines, got: %s", lines[15])
	}
}

func TestLocalBackend_Operations(t *testing.T) {
	tempDir := t.TempDir()
	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed creating .agent directory: %v", err)
	}

	configPath := filepath.Join(agentDir, "izdeploy.json")
	lockPath := filepath.Join(agentDir, "izdeploy.lock")

	initialConfig := `{
		"name": "test-service",
		"port": 8080,
		"image": "ghcr.io/org/test:v1.0.0"
	}`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("failed writing initial config: %v", err)
	}

	cfg, err := contract.ParseConfigFile(configPath)
	if err != nil {
		t.Fatalf("failed parsing config: %v", err)
	}
	lock := contract.GenerateLockfile(cfg)
	if err := contract.WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("failed writing lockfile: %v", err)
	}

	backend := NewLocalBackend(tempDir)
	ctx := context.Background()

	// 1. Test GetStatus
	status, err := backend.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.App != "test-service" || status.Port != 8080 {
		t.Errorf("unexpected status: %+v", status)
	}

	// 2. Test Deploy (image update with intact lockfile)
	deployRes, err := backend.Deploy(ctx, "ghcr.io/org/test:v1.0.1", false)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if deployRes.Status != "deployed" || deployRes.ImageTag != "ghcr.io/org/test:v1.0.1" {
		t.Errorf("unexpected deploy response: %+v", deployRes)
	}

	// Verify image in config was updated
	updatedCfg, _ := contract.ParseConfigFile(configPath)
	if updatedCfg.Image != "ghcr.io/org/test:v1.0.1" {
		t.Errorf("image was not updated in config file: %s", updatedCfg.Image)
	}

	// 3. Test SetEnv valid
	envRes, err := backend.SetEnv(ctx, map[string]string{"PORT_OVERRIDE": "8080", "APP_ENV": "stage"})
	if err != nil {
		t.Fatalf("SetEnv failed: %v", err)
	}
	if !envRes.Restarted || envRes.VariablesCount != 2 {
		t.Errorf("unexpected env response: %+v", envRes)
	}

	// 4. Test SetEnv invalid key (must return RFC 7807 problem with agent_actionable: false)
	_, err = backend.SetEnv(ctx, map[string]string{"invalid-key-with-dashes!": "value"})
	if err == nil {
		t.Fatal("expected error on invalid env key, got nil")
	}

	prob := diagnostics.ClassifyError(err)
	if prob.AgentActionable {
		t.Errorf("expected agent_actionable=false for invalid env key, got true")
	}
	if prob.ErrorCode != "ERR_INVALID_ENV_KEY" {
		t.Errorf("expected error code ERR_INVALID_ENV_KEY, got %s", prob.ErrorCode)
	}

	// 5. Test Restart
	restartRes, err := backend.Restart(ctx, 5)
	if err != nil {
		t.Fatalf("Restart failed: %v", err)
	}
	if restartRes.Status != "restarted" || restartRes.GracePeriodSeconds != 5 {
		t.Errorf("unexpected restart response: %+v", restartRes)
	}
}

func TestServer_Initialization(t *testing.T) {
	tempDir := t.TempDir()
	srv, err := NewServer(ServerOptions{
		Name:       "izdeploy-test",
		Version:    "0.1.0",
		ProjectDir: tempDir,
	})
	if err != nil {
		t.Fatalf("failed initializing MCP server: %v", err)
	}

	if srv.MCPServer() == nil {
		t.Fatal("expected non-nil underlying MCPServer")
	}
}

func TestFormatErrorResult(t *testing.T) {
	err := contract.ValidationError{Field: "port", Reason: "out of range"}
	res := FormatErrorResult(err)

	if !res.IsError {
		t.Errorf("expected IsError=true on error result")
	}

	textContent, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}

	var prob diagnostics.ProblemDetails
	if err := json.Unmarshal([]byte(textContent.Text), &prob); err != nil {
		t.Fatalf("failed parsing error result text into ProblemDetails: %v, raw=%s", err, textContent.Text)
	}

	if !prob.AgentActionable || prob.Status != 400 {
		t.Errorf("unexpected problem details in error result: %+v", prob)
	}
}
