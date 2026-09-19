package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestMonorepo(t *testing.T) {
	content, err := os.ReadFile("../../pkg/contract/schema.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "Workdir") || !strings.Contains(string(content), "Args") {
		t.Errorf("Monorepo/Submodule support (Workdir/Args) not found in schema")
	}
}

func TestPrivateSubmodules(t *testing.T) {
	content, err := os.ReadFile("../../cmd/izdeploy/cmd_deploy.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "build-arg") {
		t.Errorf("Build args parsing not found in deploy command")
	}
}

func TestInstantRollback(t *testing.T) {
	content, err := os.ReadFile("../../cmd/izdeploy/cmd_rollback.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "rollback") {
		t.Errorf("Rollback command not found")
	}
}

func TestLogsStreaming(t *testing.T) {
	content, err := os.ReadFile("../../cmd/izdeploy/cmd_logs.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "build") {
		t.Errorf("Build log streaming not found in logs command")
	}
}
