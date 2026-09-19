package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestZRAMAndSwapConfig(t *testing.T) {
	content, err := os.ReadFile("../../scripts/setup-vps-data-plane.sh")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "setup-zram.sh") {
		t.Errorf("zRAM configuration not found in setup script")
	}
}

func TestMemoryLimit(t *testing.T) {
	content, err := os.ReadFile("../../cmd/izdeploy/cmd_deploy.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "--memory-limit 1g") {
		t.Errorf("Memory limit warning/config not found in deploy command")
	}
}

func TestAutomatedGC(t *testing.T) {
	content, err := os.ReadFile("../../scripts/setup-vps-data-plane.sh")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "docker system prune") || !strings.Contains(string(content), "until=168h") {
		t.Errorf("Automated GC cronjob not found in setup script")
	}
}

func TestWorkerApps(t *testing.T) {
	content, err := os.ReadFile("../../pkg/proxy/routing.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "spec.Type == \"worker\"") {
		t.Errorf("Worker app bypass not found in proxy routing")
	}
}

func TestGhostContainersCleanup(t *testing.T) {
	content, err := os.ReadFile("../../pkg/docker/container.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "cleanupGhostContainers") {
		t.Errorf("Ghost containers cleanup not found in container client")
	}
}
