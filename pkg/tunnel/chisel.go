// Package tunnel wraps reverse tunnel client orchestration and reconnection loops.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	chclient "github.com/jpillora/chisel/client"
)

// Sentinel manager errors.
var (
	ErrTunnelAlreadyRunning = errors.New("tunnel: manager already active")
	ErrTunnelNotRunning     = errors.New("tunnel: manager is not running")
)

// DECISION: Supervise Chisel client lifecycle via an external Full Jitter goroutine loop.
// WHY: Built-in Chisel reconnect uses a basic retry strategy lacking RFC 8900 Full Jitter.
// By delegating reconnect timing to our supervisor, we eliminate thundering herd storms on Hub restart
// and retain granular control over telemetry status transitions.
// TRADE-OFF: Requires wrapping chclient.Client in an interface adapter.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-011

// Client abstracts low-level reverse tunnel operations.
type Client interface {
	Start(ctx context.Context) error
	Wait() error
	Close() error
}

// ClientFactory instantiates a configured tunnel client adapter.
type ClientFactory func(ctx context.Context, cfg Config) (Client, error)

// DefaultChiselFactory constructs a production Chisel client.
//
// Business rule: Configures outbound TLS over 443 with 1 retry before yielding back to supervisor.
//
// @ai-constraint: Sets MaxRetryCount to 1 to allow supervisor Full Jitter loop to govern delays.
func DefaultChiselFactory(_ context.Context, cfg Config) (Client, error) {
	chiselConfig := &chclient.Config{
		Server:           cfg.Server,
		Auth:             cfg.AuthToken,
		Fingerprint:      cfg.Fingerprint,
		Remotes:          cfg.Remotes,
		KeepAlive:        cfg.KeepAlive,
		MaxRetryCount:    1,
		MaxRetryInterval: cfg.MaxBackoff,
		Headers:          cfg.Headers,
		TLS: chclient.TLSConfig{
			SkipVerify: cfg.InsecureSkipVerify,
		},
	}

	rawClient, err := chclient.NewClient(chiselConfig)
	if err != nil {
		return nil, fmt.Errorf("tunnel: failed to instantiate chisel client: %w", err)
	}

	return rawClient, nil
}

// Manager supervises the reverse tunnel process, reconnect backoff, and metrics reporting.
type Manager struct {
	config       Config
	backoff      *FullJitterBackoff
	tracker      *StatusTracker
	factory      ClientFactory
	cancelFunc   context.CancelFunc
	doneChan     chan struct{}
	activeClient Client
	mutex        sync.Mutex
	isRunning    bool
}

// NewManager creates an outbound reverse tunnel manager.
//
// Business rule: Must validate parameters and initialize Full Jitter backoff.
func NewManager(cfg Config, customFactory ...ClientFactory) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	factory := DefaultChiselFactory
	if len(customFactory) > 0 && customFactory[0] != nil {
		factory = customFactory[0]
	}

	backoff := NewFullJitterBackoff(cfg.BaseBackoff, cfg.MaxBackoff)
	tracker := NewStatusTracker()

	return &Manager{
		config:   cfg,
		backoff:  backoff,
		tracker:  tracker,
		factory:  factory,
		doneChan: make(chan struct{}),
	}, nil
}

// Start launches the background supervision loop in a dedicated goroutine.
//
// Business rule: Non-blocking; returns immediately after launching supervisor goroutine.
//
// @ai-constraint: Prevent multiple concurrent background loops.
func (m *Manager) Start(parentCtx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.isRunning {
		return ErrTunnelAlreadyRunning
	}

	ctx, cancel := context.WithCancel(parentCtx)
	m.cancelFunc = cancel
	m.isRunning = true
	m.doneChan = make(chan struct{})

	go m.supervise(ctx)
	return nil
}

// Stop gracefully terminates active tunnel connections and the supervisor loop.
func (m *Manager) Stop() error {
	m.mutex.Lock()
	if !m.isRunning {
		m.mutex.Unlock()
		return ErrTunnelNotRunning
	}

	m.cancelFunc()
	active := m.activeClient
	m.mutex.Unlock()

	if active != nil {
		_ = active.Close()
	}

	<-m.doneChan

	m.mutex.Lock()
	m.isRunning = false
	m.tracker.SetState(StateClosed)
	m.mutex.Unlock()

	return nil
}

// Status returns a point-in-time telemetry snapshot of the tunnel.
func (m *Manager) Status() TunnelStatus {
	return m.tracker.Snapshot()
}

// supervise runs the continuous dial, monitoring, and Full Jitter reconnect loop.
func (m *Manager) supervise(ctx context.Context) {
	defer close(m.doneChan)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.tracker.SetState(StateConnecting)
		dialStartTime := time.Now()

		client, err := m.factory(ctx, m.config)
		if err != nil {
			m.handleFailure(ctx, err)
			continue
		}

		m.mutex.Lock()
		m.activeClient = client
		m.mutex.Unlock()

		if err := client.Start(ctx); err != nil {
			m.handleFailure(ctx, err)
			continue
		}

		latency := time.Since(dialStartTime)
		m.tracker.RecordConnected(m.config.Server, m.config.Remotes[0])
		m.tracker.RecordHeartbeat(latency)
		m.backoff.Reset()

		waitErr := client.Wait()

		m.mutex.Lock()
		m.activeClient = nil
		m.mutex.Unlock()

		if ctx.Err() != nil {
			return
		}

		m.handleFailure(ctx, waitErr)
	}
}

// handleFailure logs connection interruption, increments reconnect count, and applies backoff sleep.
func (m *Manager) handleFailure(ctx context.Context, err error) {
	m.tracker.RecordDisconnected(err)
	m.tracker.IncrementReconnect()

	delay := m.backoff.NextDelay()
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}
