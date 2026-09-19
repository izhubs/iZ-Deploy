package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/izhubs/izdeploy/pkg/mcp"
	"github.com/spf13/cobra"
)

// DECISION: Parse secrets via CLI and inject into backend.
// WHY: Prevent hardcoded secrets in the izdeploy.json file from leaking to Git.
// TRADE-OFF: Requires users to manage .env files securely.
// REF: wiki/projects/izdeploy/izdeploy_roadmap_v0.0.2.md

func newSecretCmd() *cobra.Command {
	var envFile string

	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage environment variables and secrets",
	}

	setCmd := &cobra.Command{
		Use:   "set [KEY=VALUE...]",
		Short: "Set one or more secrets/environment variables",
		RunE: func(cmd *cobra.Command, args []string) error {
			vars := make(map[string]string)

			// Load from file if provided
			if envFile != "" {
				data, err := os.ReadFile(envFile)
				if err != nil {
					return fmt.Errorf("failed to read env file: %w", err)
				}
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						key := strings.TrimSpace(parts[0])
						val := strings.TrimSpace(parts[1])
						// Remove surrounding quotes if present
						if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
							(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
							val = val[1 : len(val)-1]
						}
						vars[key] = val
					}
				}
			}

			// Parse arguments
			for _, arg := range args {
				parts := strings.SplitN(arg, "=", 2)
				if len(parts) == 2 {
					vars[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				} else {
					return fmt.Errorf("invalid format for argument %q, expected KEY=VALUE", arg)
				}
			}

			if len(vars) == 0 {
				return fmt.Errorf("no variables provided")
			}

			// Use the local backend
			ctx := context.Background()
			backend := mcp.NewLocalBackend(".")

			resp, err := backend.SetEnv(ctx, vars)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrf("SECRET ERROR: %s\n", prob.Title)
				return err
			}

			cmd.Printf("Success: %s\n", resp.Message)
			return nil
		},
	}

	setCmd.Flags().StringVar(&envFile, "file", "", "Load variables from a .env file")
	cmd.AddCommand(setCmd)

	return cmd
}
