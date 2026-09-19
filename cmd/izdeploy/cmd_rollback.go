package main

import (
	"context"
	"errors"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/izhubs/izdeploy/pkg/mcp"
	"github.com/spf13/cobra"
)

// DECISION: Rollback bypasses lockfile verification to prioritize MTTR (Mean Time To Recovery).
// WHY: During an outage, the fastest way to restore service is switching traffic to the previous healthy container.
// TRADE-OFF: Potential state mismatch if the lockfile was modified, but availability takes precedence.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-018

func newRollbackCmd() *cobra.Command {
	var (
		localMode  bool
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Instantly rollback traffic to the previous healthy container version",
		Long: `Triggers an emergency rollback by shifting proxy traffic to the immediately preceding healthy container:
- Bypasses infrastructure lockfile validation for maximum recovery speed
- Automatically updates Kamals routing table
- Restores previous image tag in the application contract`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := contract.ParseConfigFile(configPath)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrln(string(prob.JSON()))
				return errors.New("cannot read application contract")
			}

			cmd.Printf("Initiating emergency rollback for '%s'...\n", cfg.Name)

			backend := mcp.NewLocalBackend(".")
			res, err := backend.Rollback(ctx)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrf("ROLLBACK FAILED: %s\n", err.Error())
				cmd.PrintErrln(string(prob.JSON()))
				return errors.New("rollback execution failed")
			}

			cmd.Printf("Rollback successful in %dms:\n", res.DurationMS)
			cmd.Printf("  - Restored Container ID: %s\n", res.ContainerID)
			cmd.Printf("  - Restored Image Tag:    %s\n", res.ImageTag)
			cmd.Printf("  - Status:                %s\n", res.Status)
			return nil
		},
	}

	cmd.Flags().BoolVar(&localMode, "local", true, "Execute rollback against local container runtime")
	cmd.Flags().StringVar(&configPath, "config", ".agent/izdeploy.json", "Path to izDeploy configuration file")

	return cmd
}
