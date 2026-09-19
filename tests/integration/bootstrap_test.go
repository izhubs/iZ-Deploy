// Package integration verifies cross-component contracts and runtime assumptions.
package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRepositoryLayout verifies that all foundational directories and files exist.
//
// Business rule: The repository structure must adhere to IZDEPLOY-RFC-002.
//
// @ai-constraint: Do not remove structural checks; they safeguard CI/CD directory integrity.
func TestRepositoryLayout(t *testing.T) {
	requiredFiles := []string{
		"../../go.mod",
		"../../Makefile",
		"../../README.md",
		"../../LICENSE",
		"../../scripts/setup-vps-data-plane.sh",
		"../../scripts/setup-zram.sh",
		"../../scripts/systemd/izdeploy-agent.service",
		"../../scripts/systemd/izdeploy-watchdog.service",
		"../../scripts/systemd/izdeploy-watchdog.timer",
		"../../schemas/v0.1/izdeploy.schema.json",
	}

	for _, relPath := range requiredFiles {
		cleanPath := filepath.Clean(relPath)
		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			t.Errorf("expected required file %s to exist, but was not found", cleanPath)
		}
	}
}
