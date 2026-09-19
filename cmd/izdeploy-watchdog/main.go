// Package main provides the entrypoint for the izdeploy-watchdog rescue binary.
package main

import (
	"flag"
	"fmt"
	"os"
)

// Version specifies the watchdog release version.
const Version = "0.1.0-dev"

// Exit status constants.
const (
	ExitCodeSuccess = 0
	ExitCodeError   = 1
)

// DefaultFailThreshold defines consecutive failure attempts before firewall intervention.
const DefaultFailThreshold = 10

// DECISION: Build watchdog as an independent statically linked binary without CGO.
// WHY: Guarantees execution during low-memory or degraded runtime states when primary
// daemon may be unresponsive or terminated by OOM killer.
// TRADE-OFF: Binary duplicate footprint is minimal (<2MB compiled).
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-017

// main executes the watchdog connectivity verification cycle.
//
// Business rule: If reverse tunnel or agent status remains unreachable for 10
// consecutive cycles (10 minutes), watchdog opens port 22 in UFW to restore manual access.
//
// @ai-constraint: Watchdog must execute non-interactively within systemd timer window.
func main() {
	threshold := flag.Int("threshold", DefaultFailThreshold, "Failure count before emergency port 22 enable")
	dryRun := flag.Bool("dry-run", false, "Simulate checks without modifying firewall rules")
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("izdeploy-watchdog version %s\n", Version)
		os.Exit(ExitCodeSuccess)
	}

	fmt.Printf("izdeploy-watchdog v%s check executed (threshold: %d, dry-run: %t)\n", Version, *threshold, *dryRun)
	os.Exit(ExitCodeSuccess)
}
