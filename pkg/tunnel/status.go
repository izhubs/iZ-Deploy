// Package tunnel manages reverse tunnel lifecycle, metrics, and health status.
package tunnel

import (
	"encoding/json"
	"sync"
	"time"
)

// Tunnel connection lifecycle states.
const (
	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateReconnecting = "reconnecting"
	StateClosed       = "closed"
)

// DECISION: Expose tunnel telemetry through a point-in-time immutable Snapshot value object.
// WHY: Ensures external callers (such as agent health APIs and watchdog queries) read consistent
// metrics without blocking active network I/O or holding long-lived write locks.
// TRADE-OFF: Allocates a copy of TunnelStatus struct on each status query.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-011

// TunnelStatus represents the external telemetry payload of the reverse tunnel.
type TunnelStatus struct {
	State          string    `json:"state"`
	Connected      bool      `json:"connected"`
	RemoteAddr     string    `json:"remote_addr"`
	LocalAddr      string    `json:"local_addr"`
	LatencyMs      int64     `json:"latency_ms"`
	LastConnected  time.Time `json:"last_connected,omitempty"`
	LastHeartbeat  time.Time `json:"last_heartbeat,omitempty"`
	ReconnectCount uint64    `json:"reconnect_count"`
	LastError      string    `json:"last_error,omitempty"`
	UptimeSeconds  int64     `json:"uptime_seconds"`
}

// StatusTracker maintains thread-safe telemetry metrics for active reverse tunnels.
type StatusTracker struct {
	status TunnelStatus
	mutex  sync.RWMutex
}

// NewStatusTracker initializes a tracker in disconnected baseline state.
//
// Business rule: Tunnels initialize in StateDisconnected with zero uptime.
//
// @ai-constraint: Always construct via constructor to avoid uninitialized nil fields.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{
		status: TunnelStatus{
			State:     StateDisconnected,
			Connected: false,
		},
	}
}

// Snapshot returns a point-in-time copy of current tunnel metrics.
//
// Business rule: Uptime is computed dynamically against LastConnected when connected.
//
// @ai-constraint: Guarded by read lock to prevent partial updates.
func (t *StatusTracker) Snapshot() TunnelStatus {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	currentCopy := t.status
	if currentCopy.Connected && !currentCopy.LastConnected.IsZero() {
		currentCopy.UptimeSeconds = int64(time.Since(currentCopy.LastConnected).Seconds())
	}
	return currentCopy
}

// SetState updates the high-level connection state.
func (t *StatusTracker) SetState(newState string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.status.State = newState
	t.status.Connected = (newState == StateConnected)
}

// RecordConnected marks a successful tunnel establishment.
//
// Business rule: Resets last error, sets connected flag, and updates connection timestamp.
//
// @ai-constraint: Must be called inside dial loop immediately after successful handshake.
func (t *StatusTracker) RecordConnected(remoteAddr, localAddr string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := time.Now().UTC()
	t.status.State = StateConnected
	t.status.Connected = true
	t.status.RemoteAddr = remoteAddr
	t.status.LocalAddr = localAddr
	t.status.LastConnected = now
	t.status.LastHeartbeat = now
	t.status.LastError = ""
}

// RecordDisconnected records an unexpected tunnel termination.
//
// Business rule: Retains historical reconnect counter and flags connected as false.
func (t *StatusTracker) RecordDisconnected(err error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.status.State = StateDisconnected
	t.status.Connected = false
	t.status.UptimeSeconds = 0
	if err != nil {
		t.status.LastError = err.Error()
	}
}

// RecordHeartbeat logs successful ping roundtrip duration.
//
// Business rule: Latency must reflect round-trip time in milliseconds.
func (t *StatusTracker) RecordHeartbeat(latency time.Duration) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.status.LastHeartbeat = time.Now().UTC()
	t.status.LatencyMs = latency.Milliseconds()
}

// IncrementReconnect records a reconnection attempt cycle.
func (t *StatusTracker) IncrementReconnect() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.status.ReconnectCount++
	t.status.State = StateReconnecting
}

// ToJSON serializes the status snapshot into formatted JSON bytes.
func (t *StatusTracker) ToJSON() ([]byte, error) {
	snapshot := t.Snapshot()
	return json.Marshal(snapshot)
}
