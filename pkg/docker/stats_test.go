// Package docker_test verifies resource metrics calculation and caching dynamics.
package docker_test

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/izhubs/izdeploy/pkg/docker"
)

func TestCalculateMetrics(t *testing.T) {
	mockStats := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000,
			},
			SystemUsage: 2000000000,
			OnlineCPUs:  2,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 400000000,
			},
			SystemUsage: 1000000000,
		},
		MemoryStats: container.MemoryStats{
			Usage: 256 * 1024 * 1024,
			Limit: 512 * 1024 * 1024,
			Stats: map[string]uint64{
				"inactive_file": 16 * 1024 * 1024,
			},
		},
	}

	metrics := docker.CalculateMetrics(mockStats)

	// cpuDelta = 100000000, systemDelta = 1000000000, onlineCPUs = 2
	// cpuPercent = (100000000 / 1000000000) * 2 * 100 = 20.0%
	if metrics.CPUPercent < 19.99 || metrics.CPUPercent > 20.01 {
		t.Errorf("expected CPU ~20.0%%, got %f%%", metrics.CPUPercent)
	}

	// memoryUsage = 256MB - 16MB = 240MB
	expectedMem := uint64((256 - 16) * 1024 * 1024)
	if metrics.MemoryUsageBytes != expectedMem {
		t.Errorf("expected memory %d bytes, got %d", expectedMem, metrics.MemoryUsageBytes)
	}

	expectedLimit := uint64(512 * 1024 * 1024)
	if metrics.MemoryLimitBytes != expectedLimit {
		t.Errorf("expected memory limit %d bytes, got %d", expectedLimit, metrics.MemoryLimitBytes)
	}
}

func TestMetricsCache_TTL(t *testing.T) {
	cache := docker.NewMetricsCache()
	containerID := "test-container-id"

	metrics := &docker.ContainerMetrics{
		CPUPercent:       15.5,
		MemoryUsageBytes: 1024,
		MemoryLimitBytes: 2048,
		Timestamp:        time.Now().UTC(),
	}

	// Test cache hit
	cache.Set(containerID, metrics, 50*time.Millisecond)
	retrieved, found := cache.Get(containerID)
	if !found {
		t.Fatalf("expected metrics in cache, but was not found")
	}
	if retrieved.CPUPercent != 15.5 {
		t.Errorf("expected 15.5%%, got %f%%", retrieved.CPUPercent)
	}

	// Test cache expiration
	time.Sleep(60 * time.Millisecond)
	_, foundAfterExpiry := cache.Get(containerID)
	if foundAfterExpiry {
		t.Errorf("expected metrics to expire, but still found in cache")
	}
}
