// Package docker provides an enterprise-grade Go SDK wrapper around the Docker Engine API.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
)

// DefaultMetricsTTL defines the cache duration to prevent hammering Docker daemon on frequent poll requests.
const DefaultMetricsTTL = 5 * time.Second

// ContainerMetrics captures point-in-time resource consumption derived from cgroups v2.
type ContainerMetrics struct {
	CPUPercent       float64   `json:"cpu_percent"`
	MemoryUsageBytes uint64    `json:"memory_usage_bytes"`
	MemoryLimitBytes uint64    `json:"memory_limit_bytes"`
	Timestamp        time.Time `json:"timestamp"`
}

type metricsCacheEntry struct {
	metrics   *ContainerMetrics
	expiresAt time.Time
}

// MetricsCache provides thread-safe TTL caching for container telemetry.
type MetricsCache struct {
	entries map[string]metricsCacheEntry
	mutex   sync.RWMutex
}

// NewMetricsCache instantiates an empty telemetry cache.
func NewMetricsCache() *MetricsCache {
	return &MetricsCache{
		entries: make(map[string]metricsCacheEntry),
	}
}

// Get retrieves a valid unexpired metrics snapshot if present.
func (mc *MetricsCache) Get(containerID string) (*ContainerMetrics, bool) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	entry, found := mc.entries[containerID]
	if !found || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.metrics, true
}

// Set stores a calculated metrics snapshot with the specified TTL.
func (mc *MetricsCache) Set(containerID string, metrics *ContainerMetrics, ttl time.Duration) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	mc.entries[containerID] = metricsCacheEntry{
		metrics:   metrics,
		expiresAt: time.Now().Add(ttl),
	}
}

var globalMetricsCache = NewMetricsCache()

// DECISION: Cache container resource metrics with a 5-second sliding TTL window.
// WHY: Docker stats inspection parses kernel cgroups files; excessive polling degrades I/O latency.
// TRADE-OFF: Telemetry data can be up to 5 seconds stale.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-010

// ContainerStatsOneShot retrieves CPU and Memory utilization using non-streaming inspection.
//
// Business rule: Stats must return cached metrics when invoked within the 5s TTL window.
//
// @ai-constraint: Never use streaming ContainerStats in request-response HTTP endpoints.
func (c *Client) ContainerStatsOneShot(ctx context.Context, containerID string) (*ContainerMetrics, error) {
	if cached, ok := globalMetricsCache.Get(containerID); ok {
		return cached, nil
	}

	statsResponse, err := c.cli.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed querying stats for %s: %w", containerID, err)
	}
	defer func() { _ = statsResponse.Body.Close() }()

	var statsJSON container.StatsResponse
	if err := json.NewDecoder(statsResponse.Body).Decode(&statsJSON); err != nil {
		return nil, fmt.Errorf("failed decoding container stats JSON: %w", err)
	}

	metrics := CalculateMetrics(&statsJSON)
	globalMetricsCache.Set(containerID, metrics, DefaultMetricsTTL)
	return metrics, nil
}

// CalculateMetrics computes CPU percentage and cgroups v2 memory usage from raw Docker stats.
//
// Business rule: CPU percent calculation uses the standard delta formula across system and container ticks.
func CalculateMetrics(stats *container.StatsResponse) *ContainerMetrics {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)

	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if onlineCPUs == 0 {
		onlineCPUs = 1.0
	}

	var cpuPercent float64
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}

	memoryUsage := stats.MemoryStats.Usage
	if inactiveFile, ok := stats.MemoryStats.Stats["inactive_file"]; ok && memoryUsage > inactiveFile {
		memoryUsage -= inactiveFile
	} else if totalInactive, ok := stats.MemoryStats.Stats["total_inactive_file"]; ok && memoryUsage > totalInactive {
		memoryUsage -= totalInactive
	}

	return &ContainerMetrics{
		CPUPercent:       cpuPercent,
		MemoryUsageBytes: memoryUsage,
		MemoryLimitBytes: stats.MemoryStats.Limit,
		Timestamp:        time.Now().UTC(),
	}
}
