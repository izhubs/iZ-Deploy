// Package main provides the CLI entrypoint and Model Context Protocol stdio server for izDeploy.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version specifies the binary release version injected at link time.
const Version = "0.1.0-dev"

// Exit status constants.
const (
	ExitCodeSuccess = 0
	ExitCodeError   = 1
)

// DECISION: Migrate CLI commands to Cobra structure for unified developer tooling (TASK-013).
// WHY: Cobra provides declarative subcommand registration, POSIX flag parsing, automated shell completion,
// and consistent help output across developer terminals and IDE runners.
// TRADE-OFF: Adds spf13/cobra dependency tree to the CLI binary.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-013

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "izdeploy",
		Short: "izDeploy application contract validator, deployment engine, and MCP stdio server",
		Long: `izDeploy is a lightweight PaaS engine and AI-agent deployment daemon optimized for resource-constrained Linux environments (>=512MB RAM).

Available Commands:
  init    Initialize a minimal .agent/izdeploy.json contract, lockfile, and AI bridge rules
  lint    Validate application contract syntax, semantic constraints, and SHA-256 lockfile
  deploy  Execute container image deployment with pre-deploy lock integrity verification
  mcp     Launch the stdio Model Context Protocol server exposing 5 deployment tools
  status  Inspect runtime state, container health, and resource consumption`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("izdeploy version {{.Version}}\n")

	// Register modular subcommands
	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newLintCmd())
	rootCmd.AddCommand(newDeployCmd())
	rootCmd.AddCommand(newMcpCmd())
	rootCmd.AddCommand(newStatusCmd())

	return rootCmd
}

// main initializes command dispatch for the izdeploy CLI.
//
// Business rule: The CLI serves dual roles as an interactive terminal tool and an automated MCP stdio provider.
//
// @ai-constraint: Never output non-JSON text to stdout when operating in MCP mode.
func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(ExitCodeError)
	}
}
