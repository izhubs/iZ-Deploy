// Package main provides the host daemon and internal HTTP control API for izdeploy.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/izhubs/izdeploy/internal/pocketbase"
	"github.com/izhubs/izdeploy/pkg/docker"
)

// Server configuration timeouts.
const (
	ServerReadTimeoutSeconds  = 15
	ServerWriteTimeoutSeconds = 30
	ServerIdleTimeoutSeconds  = 60
)

// DECISION: Internal HTTP API uses standard library net/http rather than heavy third-party routing.
// WHY: Reduces binary bloat, avoids framework lock-in, and isolates daemon control endpoints.
// TRADE-OFF: Routing must be registered via ServeMux pattern matching.
// REF: wiki/projects/izdeploy/izdeploy_repo_structure.md#repo-1-izhubsizdeploy-oss-core

// AgentServer orchestrates HTTP endpoints exposed exclusively on localhost.
//
// Business rule: The API must never bind to public 0.0.0.0 interfaces; internal node operations only.
type AgentServer struct {
	httpServer   *http.Server
	storage      *pocketbase.Engine
	dockerClient *docker.Client
}

// NewAgentServer constructs and registers all routing handlers for the daemon API.
//
// @ai-constraint: Verify all endpoints enforce method validation and timeout propagation.
func NewAgentServer(listenAddr string, storage *pocketbase.Engine, dockerClient *docker.Client) *AgentServer {
	server := &AgentServer{
		storage:      storage,
		dockerClient: dockerClient,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/status", server.handleStatus)
	mux.HandleFunc("/deploy", server.handleDeploy)
	mux.HandleFunc("/restart", server.handleRestart)
	mux.HandleFunc("/logs", server.handleLogs)
	mux.HandleFunc("/env", server.handleEnv)
	mux.HandleFunc("/webhook", server.handleWebhook)
	mux.HandleFunc("/webhook/deploy", server.handleWebhook)

	server.httpServer = &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  ServerReadTimeoutSeconds * time.Second,
		WriteTimeout: ServerWriteTimeoutSeconds * time.Second,
		IdleTimeout:  ServerIdleTimeoutSeconds * time.Second,
	}

	return server
}

// Start initiates listening for inbound control requests.
func (s *AgentServer) Start() error {
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("agent server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully terminates in-flight HTTP requests within the specified context deadline.
func (s *AgentServer) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
