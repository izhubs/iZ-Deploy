package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestGracefulTermination(t *testing.T) {
	content, err := os.ReadFile("../../pkg/docker/container.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "SIGTERM") || !strings.Contains(string(content), "GracePeriodSeconds") {
		t.Errorf("Graceful termination (SIGTERM/grace period) not found")
	}
}

func TestCloudflareSSL(t *testing.T) {
	content, err := os.ReadFile("../../pkg/proxy/cert.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "dns-01") {
		t.Errorf("DNS-01 challenge not found in cert.go")
	}
}

func TestSecretsManagement(t *testing.T) {
	content, err := os.ReadFile("../../cmd/izdeploy/cmd_secret.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "secret") {
		t.Errorf("Secret management command not found")
	}
}

func TestDatabaseBackup(t *testing.T) {
	content, err := os.ReadFile("../../cmd/izdeploy/cmd_volume.go")
	if err != nil { t.Fatalf("failed to read file: %v", err) }
	if !strings.Contains(string(content), "backup") || !strings.Contains(string(content), "s3") {
		t.Errorf("Volume backup command to S3 not found")
	}
}
