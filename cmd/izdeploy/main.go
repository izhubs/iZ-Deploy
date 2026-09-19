// Package main provides the entrypoint for the izdeploy developer CLI.
package main

import (
	"flag"
	"fmt"
	"os"
)

// Version specifies the binary release version injected at link time.
const Version = "0.1.0-dev"

// ExitCodeSuccess and standard POSIX exit status constants.
const (
	ExitCodeSuccess = 0
	ExitCodeError   = 1
)

// DECISION: Use stdlib flag parser for bootstrap scaffolding before Cobra introduction.
// WHY: Avoid external dependencies during initial monorepo foundation phase (TASK-001)
// while providing immediate syntax validation and status display.
// TRADE-OFF: Subcommands are mapped manually until Cobra CLI wiring in TASK-013.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-001

// main initializes command dispatch for the izdeploy CLI.
//
// Business rule: The CLI acts as both a local developer tool and an MCP stdio server.
//
// @ai-constraint: Maintain zero external dependencies in the root cmd bootstrap
// to guarantee clean initial module compilation across all platforms.
func main() {
	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("izdeploy CLI version %s\n", Version)
		os.Exit(ExitCodeSuccess)
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Printf("izdeploy CLI v%s\nUsage: izdeploy [command] [options]\nCommands: init, lint, deploy, mcp, status\n", Version)
		os.Exit(ExitCodeSuccess)
	}

	command := args[0]
	switch command {
	case "version":
		fmt.Printf("izdeploy version %s\n", Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		os.Exit(ExitCodeError)
	}
}
