package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_EndToEndLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed getting current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed changing to temp dir: %v", err)
	}

	// 1. Test 'init' command
	rootCmd := newRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"init", "--name", "e2e-app", "--port", "3000", "--image", "ghcr.io/org/e2e:v1.0.0"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init command failed: %v, output: %s", err, buf.String())
	}

	configPath := filepath.Join(".agent", "izdeploy.json")
	lockPath := filepath.Join(".agent", "izdeploy.lock")

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created at %s", configPath)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file was not created at %s", lockPath)
	}

	// 2. Test 'lint' command (should pass on freshly initialized project)
	rootCmd = newRootCmd()
	buf.Reset()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"lint"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("lint command failed on valid project: %v, output: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "OK: .agent/izdeploy.json conforms to Schema v0.1") {
		t.Errorf("unexpected lint output: %s", buf.String())
	}

	// 3. Test 'status' command
	rootCmd = newRootCmd()
	buf.Reset()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("status command failed: %v, output: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "Application Status: e2e-app") {
		t.Errorf("unexpected status output: %s", buf.String())
	}

	// 4. Test 'deploy' command
	rootCmd = newRootCmd()
	buf.Reset()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"deploy", "--image", "ghcr.io/org/e2e:v1.0.1"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deploy command failed: %v, output: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "Deployment successful") {
		t.Errorf("unexpected deploy output: %s", buf.String())
	}

	// 5. Test 'logs' command (auto-detect app name from .agent/izdeploy.json)
	rootCmd = newRootCmd()
	buf.Reset()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"logs", "--tail", "20", "--local"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("logs command failed: %v, output: %s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "e2e-app") {
		t.Errorf("expected logs output to contain 'e2e-app', got: %s", buf.String())
	}
}

func TestCLI_LogsSubcommand(t *testing.T) {
	tempDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed getting cwd: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed chdir: %v", err)
	}

	// Case 1: Error when no config exists and no --app provided
	rootCmd := newRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"logs"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error when no app and no config, got nil")
	}

	// Case 2: Succeed when --app is explicitly passed with --local
	rootCmd = newRootCmd()
	buf.Reset()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"logs", "--app", "explicit-app", "--tail", "10", "--local"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("logs with explicit --app failed: %v, output: %s", err, buf.String())
	}
	if len(buf.String()) == 0 {
		t.Errorf("expected non-empty log output")
	}
}

func TestCLI_Version(t *testing.T) {
	rootCmd := newRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	if !strings.Contains(buf.String(), Version) {
		t.Errorf("expected version %s in output, got: %s", Version, buf.String())
	}
}
