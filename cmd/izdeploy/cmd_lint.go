package main

import (
	"errors"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/spf13/cobra"
)

// DECISION: Format lint errors with line-precise pointers or RFC 7807 JSON.
// WHY: Ensures developer terminal displays immediate actionable error locations (file:line:col)
// while enabling programmatic consumers and AI tools to parse structured ProblemDetails.
// TRADE-OFF: Dual output rendering paths depending on --json flag.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-013

func newLintCmd() *cobra.Command {
	var (
		configPath string
		lockPath   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate application contract schema and SHA-256 lockfile integrity",
		Long: `Performs static analysis and integrity validation on the izDeploy contract manifest:
1. Verifies JSON syntax and line-number correctness.
2. Validates semantic constraints (DNS-safe names, valid TCP ports, healthchecks).
3. Verifies SHA-256 cryptographic signatures against .agent/izdeploy.lock.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := contract.ParseConfigFile(configPath)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				if jsonOutput {
					cmd.Println(string(prob.JSON()))
				} else {
					var syntaxErr *contract.SyntaxError
					if errors.As(err, &syntaxErr) {
						cmd.PrintErrf("LINT ERROR: %s at %s:%d:%d\n", syntaxErr.Message, configPath, syntaxErr.Line, syntaxErr.Column)
					} else {
						cmd.PrintErrf("LINT ERROR: %s: %s\n", configPath, err.Error())
					}
				}
				return errors.New("contract linting failed")
			}

			// Validate semantic constraints
			if err := contract.Validate(cfg); err != nil {
				prob := diagnostics.ClassifyError(err)
				if jsonOutput {
					cmd.Println(string(prob.JSON()))
				} else {
					var valErrs contract.ValidationErrors
					if errors.As(err, &valErrs) {
						for _, ve := range valErrs {
							cmd.PrintErrf("LINT ERROR: %s: %s\n", configPath, ve.Error())
						}
					} else {
						cmd.PrintErrf("LINT ERROR: %s: %s\n", configPath, err.Error())
					}
				}
				return errors.New("semantic constraint validation failed")
			}

			// Verify lockfile
			lockRes, err := contract.VerifyLockfile(lockPath, cfg)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				if jsonOutput {
					cmd.Println(string(prob.JSON()))
				} else {
					cmd.PrintErrf("LOCKFILE ERROR: %s\n", err.Error())
				}
				return errors.New("lockfile integrity verification failed")
			}

			if jsonOutput {
				cmd.Println(`{"status": "valid", "schema": "v0.1", "agent_actionable": true}`)
			} else {
				cmd.Printf("OK: %s conforms to Schema v0.1\n", configPath)
				cmd.Printf("  - App Name:  %s\n", cfg.Name)
				cmd.Printf("  - Port:      %d\n", cfg.Port)
				cmd.Printf("  - Image:     %s\n", cfg.Image)
				cmd.Printf("  - Lockfile:  %s (verified, infra_hash=%s)\n", lockPath, lockRes.ExpectedInfra[:8])
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", ".agent/izdeploy.json", "Path to izDeploy configuration file")
	cmd.Flags().StringVar(&lockPath, "lock", contract.DefaultLockfileName, "Path to izDeploy lockfile")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output validation result as RFC 7807 / JSON Problem Details")

	return cmd
}
