// Package main provides the entrypoint for the izdeploy-agent host daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/izhubs/izdeploy/internal/pocketbase"
	"github.com/izhubs/izdeploy/pkg/docker"
)

// Version specifies the daemon release version injected at link time.
var (
	Version = "0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

// Exit status constants.
const (
	ExitCodeSuccess = 0
	ExitCodeError   = 1
)

// DefaultListenAddr defines the default local HTTP listener for internal control calls.
const DefaultListenAddr = "127.0.0.1:8098"

// DefaultShutdownTimeout specifies the grace period to drain connections before exit.
const DefaultShutdownTimeout = 15 * time.Second

// DECISION: Embed daemon signal interception and shutdown sequencing in main.
// WHY: Ensures SIGTERM triggers graceful HTTP connection draining, WAL log flushing,
// and Docker socket release before process termination.
// TRADE-OFF: Blocks main goroutine until OS signal arrives.
// REF: wiki/projects/izdeploy/izdeploy_repo_structure.md#repo-1-izhubsizdeploy-oss-core

// main boots the izdeploy node daemon on the target host.
//
// Business rule: The agent must execute under unprivileged user 'izdeploy'
// and connect to local Docker daemon over unix socket.
//
// @ai-constraint: Do not escalate privileges within the daemon process;
// host boundary operations are delegated to dedicated helper units.
func main() {
	addr := flag.String("listen", DefaultListenAddr, "Local internal HTTP API listen address")
	dataDir := flag.String("data-dir", resolveDefaultDataDir(), "Directory for embedded SQLite storage")
	dockerHost := flag.String("docker-host", "", "Docker daemon address (default unix:///var/run/docker.sock or DOCKER_HOST)")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("izdeploy-agent version %s\n", Version)
		os.Exit(ExitCodeSuccess)
	}

	fmt.Printf("izdeploy-agent v%s starting on %s (data: %s)\n", Version, *addr, *dataDir)

	storage, err := pocketbase.NewEngine(*dataDir)
	if err != nil {
		log.Fatalf("failed initializing database: %v", err)
	}
	defer func() { _ = storage.Close() }()

	dockerClient, err := docker.NewClient(*dockerHost)
	if err != nil {
		log.Fatalf("failed initializing docker client: %v", err)
	}
	defer func() { _ = dockerClient.Close() }()

	server := NewAgentServer(*addr, storage, dockerClient)
	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("izdeploy-agent received signal %v, shutting down gracefully\n", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("error during server shutdown: %v", err)
	}

	fmt.Println("izdeploy-agent stopped")
	os.Exit(ExitCodeSuccess)
}

func resolveDefaultDataDir() string {
	linuxStandardDir := "/var/lib/izdeploy/pb_data"
	if err := os.MkdirAll(filepath.Dir(linuxStandardDir), 0755); err == nil {
		return linuxStandardDir
	}
	return "./pb_data"
}
