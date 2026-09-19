package main

import (
	"context"
	"encoding/json"

	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/izhubs/izdeploy/pkg/mcp"
	"github.com/spf13/cobra"
)

// DECISION: Support both human-readable ASCII summary and structured JSON in status command.
// WHY: Developers reading terminal want instant glanceable stats; CI/CD scripts require clean JSON.
// TRADE-OFF: Dual formatting branches in CLI logic.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-010

func newStatusCmd() *cobra.Command {
	var (
		jsonOutput bool
		projectDir string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect runtime state, container health, and resource consumption",
		Long: `Queries the local or remote izDeploy deployment backend to retrieve:
- Current container execution state (running, stopped, degraded)
- System uptime
- Memory consumption (RAM MB)
- CPU utilization percentage
- Bound internal port and image reference`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			backend := mcp.NewLocalBackend(projectDir)

			status, err := backend.GetStatus(ctx)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				if jsonOutput {
					cmd.Println(string(prob.JSON()))
				} else {
					cmd.PrintErrf("STATUS ERROR: %s\n", err.Error())
				}
				return err
			}

			if jsonOutput {
				data, err := json.MarshalIndent(status, "", "  ")
				if err != nil {
					return err
				}
				cmd.Println(string(data))
			} else {
				cmd.Printf("Application Status: %s\n", status.App)
				cmd.Printf("  - State:       %s\n", status.Status)
				cmd.Printf("  - Internal Port: %d\n", status.Port)
				cmd.Printf("  - Image:       %s\n", status.Image)
				cmd.Printf("  - Uptime:      %s\n", status.Uptime)
				cmd.Printf("  - RAM Usage:   %.1f MB\n", status.RAMMB)
				cmd.Printf("  - CPU Usage:   %.1f%%\n", status.CPUPercent)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output status report in structured JSON format")
	cmd.Flags().StringVar(&projectDir, "dir", ".", "Root workspace directory containing .agent/izdeploy.json")

	return cmd
}
