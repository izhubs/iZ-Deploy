// Package main provides the entrypoint for the izdeploy-watchdog rescue binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Version specifies the watchdog release version injected at link time.
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

// DECISION: Build watchdog as an independent statically linked binary without CGO.
// WHY: Guarantees execution during low-memory or degraded runtime states when primary
// daemon may be unresponsive or terminated by OOM killer.
// TRADE-OFF: Binary duplicate footprint is minimal (<2MB compiled).
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-017

// main boots the rescue watchdog and evaluates connectivity health.
//
// Business rule: If reverse tunnel or agent status remains unreachable for 10
// consecutive cycles (10 minutes), watchdog opens port 22 in UFW to restore manual access.
//
// @ai-constraint: Watchdog must execute non-interactively within systemd timer window.
func main() {
	endpoint := flag.String("endpoint", DefaultHeartbeatEndpoint, "Control plane or agent health probe URL")
	token := flag.String("token", "", "Authentication bearer token for health probe")
	threshold := flag.Int("threshold", DefaultFailThreshold, "Failure count before emergency port 22 enable")
	stateFile := flag.String("state-file", DefaultStatePath, "Path to persistent failure state file")
	logFile := flag.String("log-file", DefaultLogFilePath, "Path to emergency watchdog log file")
	dryRun := flag.Bool("dry-run", false, "Simulate checks without modifying firewall rules")
	isDaemon := flag.Bool("daemon", false, "Run in continuous loop mode instead of oneshot timer")
	interval := flag.Duration("interval", 60*time.Second, "Check interval when running in daemon mode")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("izdeploy-watchdog version %s\n", Version)
		os.Exit(ExitCodeSuccess)
	}

	cfg := EngineConfig{
		Endpoint:      *endpoint,
		AuthToken:     *token,
		FailThreshold: *threshold,
		StateFilePath: *stateFile,
		LogFilePath:   *logFile,
		DryRun:        *dryRun,
		Timeout:       DefaultCheckTimeout,
	}

	var firewall FirewallController
	if *dryRun {
		firewall = &MockFirewall{}
	} else {
		firewall = &UFWFirewall{}
	}

	checker := &HTTPLivenessChecker{}
	engine := NewEngine(cfg, checker, firewall)

	if !*isDaemon {
		if err := engine.Evaluate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "watchdog evaluation error: %v\n", err)
			os.Exit(ExitCodeError)
		}
		os.Exit(ExitCodeSuccess)
	}

	runDaemonLoop(engine, *interval)
}

// runDaemonLoop executes periodic checks until interrupted by OS termination signals.
func runDaemonLoop(engine *Engine, interval time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial check on boot
	_ = engine.Evaluate(ctx)

	for {
		select {
		case <-sigChan:
			fmt.Println("watchdog daemon terminating")
			os.Exit(ExitCodeSuccess)
		case <-ticker.C:
			_ = engine.Evaluate(ctx)
		}
	}
}
