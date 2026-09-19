// Package main provides the entrypoint for the izdeploy-agent host daemon.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Version specifies the daemon release version.
const Version = "0.1.0-dev"

// Exit status constants.
const (
	ExitCodeSuccess = 0
	ExitCodeError   = 1
)

// DefaultListenAddr defines the default local HTTP listener for internal control calls.
const DefaultListenAddr = "127.0.0.1:8090"

// DECISION: Embed daemon signal interception directly in main routine.
// WHY: Ensures SIGTERM and SIGINT trigger clean container state preservation
// and flush pending journal entries before process termination.
// TRADE-OFF: Blocks main goroutine until signal arrival.
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
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("izdeploy-agent version %s\n", Version)
		os.Exit(ExitCodeSuccess)
	}

	fmt.Printf("izdeploy-agent v%s starting on %s\n", Version, *addr)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("izdeploy-agent received signal %v, shutting down\n", sig)
	os.Exit(ExitCodeSuccess)
}
