package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/izhubs/izdeploy/pkg/mcp"
	"github.com/spf13/cobra"
)

// DECISION: Run MCP server strictly over stdio transport.
// WHY: Conforms directly to Anthropic Model Context Protocol specification for IDE sidecars,
// allowing seamless configuration inside cursor settings, claude_desktop_config.json, etc.
// TRADE-OFF: Standard output is exclusively reserved for JSON-RPC 2.0 frames; all diagnostics go to stderr.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

func newMcpCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Launch stdio Model Context Protocol server exposing 5 deployment tools",
		Long: `Starts the izDeploy MCP server over standard input/output (stdio).
Registers 5 automated deployment tools:
  - iz_status: inspect uptime, RAM usage, and application state
  - iz_deploy: trigger image deployment with pre-deploy lock verification
  - iz_logs: ring-buffered 60-line log stream with ANSI stripping
  - iz_restart: graceful application container restart with connection draining
  - iz_env: safe environment variable updates`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			srv, err := mcp.NewServer(mcp.ServerOptions{
				Name:       "izdeploy",
				Version:    Version,
				ProjectDir: projectDir,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed initializing MCP server: %v\n", err)
				return err
			}

			fmt.Fprintf(os.Stderr, "izdeploy MCP stdio server v%s listening on stdio (project: %s)\n", Version, projectDir)
			return srv.ServeStdio(ctx)
		},
	}

	cmd.Flags().StringVar(&projectDir, "dir", ".", "Root workspace directory containing .agent/izdeploy.json")

	return cmd
}
