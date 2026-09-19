// Package docker_test verifies container configuration builders and lifecycle parameters.
package docker_test

import (
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/izhubs/izdeploy/pkg/docker"
)

func TestBuildContainerConfigs(t *testing.T) {
	spec := docker.DeploySpec{
		Name:   "api-service",
		Image:  "ghcr.io/org/api:v1.2.3",
		Port:   8080,
		HostIP: "127.0.0.1",
		Env: map[string]string{
			"APP_ENV":  "production",
			"LOG_TYPE": "json",
		},
		Volumes: map[string]string{
			"/var/data": "/app/data",
		},
		MemoryLimitMB: 256,
		CPULimit:      1.5,
		Labels: map[string]string{
			"managed-by": "izdeploy",
		},
	}

	containerConfig, hostConfig, err := docker.BuildContainerConfigs(spec)
	if err != nil {
		t.Fatalf("failed building configs: %v", err)
	}

	// 1. Image and Labels
	if containerConfig.Image != "ghcr.io/org/api:v1.2.3" {
		t.Errorf("expected image %s, got %s", spec.Image, containerConfig.Image)
	}
	if containerConfig.Labels["managed-by"] != "izdeploy" {
		t.Errorf("expected label managed-by=izdeploy, got %v", containerConfig.Labels)
	}

	// 2. Port exposure and binding
	expectedPort := nat.Port("8080/tcp")
	if _, ok := containerConfig.ExposedPorts[expectedPort]; !ok {
		t.Errorf("port 8080/tcp not exposed")
	}
	bindings, ok := hostConfig.PortBindings[expectedPort]
	if !ok || len(bindings) == 0 {
		t.Fatalf("port binding missing for %s", expectedPort)
	}
	if bindings[0].HostIP != "127.0.0.1" || bindings[0].HostPort != "8080" {
		t.Errorf("unexpected port binding: %+v", bindings[0])
	}

	// 3. Cgroups v2 limits
	expectedMemoryBytes := int64(256 * 1024 * 1024)
	if hostConfig.Resources.Memory != expectedMemoryBytes {
		t.Errorf("expected memory %d bytes, got %d", expectedMemoryBytes, hostConfig.Resources.Memory)
	}
	expectedNanoCPUs := int64(1.5 * 1e9)
	if hostConfig.Resources.NanoCPUs != expectedNanoCPUs {
		t.Errorf("expected nano CPUs %d, got %d", expectedNanoCPUs, hostConfig.Resources.NanoCPUs)
	}

	// 4. Volume binds
	if len(hostConfig.Binds) != 1 || hostConfig.Binds[0] != "/var/data:/app/data:rw" {
		t.Errorf("unexpected volume binds: %+v", hostConfig.Binds)
	}

	// 5. Restart policy
	if hostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Errorf("expected restart policy unless-stopped, got %s", hostConfig.RestartPolicy.Name)
	}
}
