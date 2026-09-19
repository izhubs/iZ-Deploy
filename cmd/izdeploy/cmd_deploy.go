package main

import (
	"context"
	"errors"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/izhubs/izdeploy/pkg/mcp"
	"github.com/spf13/cobra"
)

// DECISION: Block deploy execution immediately if lockfile verification detects modified infra without force.
// WHY: Hard architectural gate preventing accidental outages or port hijacking.
// TRADE-OFF: Developer must explicitly pass --force when port or volume changes are intentional.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-012

func newDeployCmd() *cobra.Command {
	var (
		localMode  bool
		force      bool
		imageTag   string
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy container image with zero-downtime swap and pre-deploy lock verification",
		Long: `Triggers container deployment for the application defined in .agent/izdeploy.json:
- Verifies SHA-256 infrastructure lockfile (.agent/izdeploy.lock)
- Validates that ports, routes, and resources have not been altered without --force
- Updates container image tag and signals the host agent for zero-downtime routing swap`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := contract.ParseConfigFile(configPath)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrln(string(prob.JSON()))
				return errors.New("cannot read application contract")
			}

			if imageTag == "" {
				imageTag = cfg.Image
			}

			// Pre-deploy lockfile verification
			lockRes, err := contract.VerifyLockfile(contract.DefaultLockfileName, cfg)
			if err != nil && !force {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrf("PRE-DEPLOY GATE FAILED: %s\n", err.Error())
				cmd.PrintErrln(string(prob.JSON()))
				return errors.New("deployment rejected: infrastructure locked")
			}

			if lockRes != nil && lockRes.InfraChanged && !force {
				prob := diagnostics.NewInfraLockedProblem(lockRes.Message)
				cmd.PrintErrln(string(prob.JSON()))
				return errors.New("deployment rejected: infrastructure modified")
			}

			cmd.Printf("Starting deployment for '%s' (image: %s)...\n", cfg.Name, imageTag)

			backend := mcp.NewLocalBackend(".")
			res, err := backend.Deploy(ctx, imageTag, force)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrf("DEPLOYMENT FAILED: %s\n", err.Error())
				cmd.PrintErrln(string(prob.JSON()))
				return errors.New("deployment execution failed")
			}

			cmd.Printf("Deployment successful in %dms:\n", res.DurationMS)
			cmd.Printf("  - Container ID: %s\n", res.ContainerID)
			cmd.Printf("  - Image Tag:    %s\n", res.ImageTag)
			cmd.Printf("  - Status:       %s\n", res.Status)
			return nil
		},
	}

	cmd.Flags().BoolVar(&localMode, "local", true, "Execute deployment against local container runtime")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Bypass infrastructure lockfile verification gate")
	cmd.Flags().StringVar(&imageTag, "image", "", "Override container image reference for this deployment")
	cmd.Flags().StringVar(&configPath, "config", ".agent/izdeploy.json", "Path to izDeploy configuration file")

	return cmd
}
