package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/spf13/cobra"
)

// DECISION: Generate minimal manifest under 10 lines during init.
// WHY: Lowers cognitive load for vibe coders and maximizes token efficiency during AI ingestion.
// TRADE-OFF: Advanced fields (cgroups, routes) must be added manually or via subsequent CLI flags.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-006

func newInitCmd() *cobra.Command {
	var (
		appName string
		appPort int
		image   string
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new .agent/izdeploy.json contract, lockfile, and AI bridge rules",
		Long: `Initializes the izDeploy workspace contract in the current directory:
- Creates .agent/izdeploy.json (< 10 lines template)
- Generates .agent/izdeploy.lock with cryptographic SHA-256 signatures
- Generates AI governance rules (.cursor/rules/izdeploy.mdc and CLAUDE.md)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			agentDir := ".agent"
			configPath := filepath.Join(agentDir, "izdeploy.json")
			lockPath := filepath.Join(agentDir, "izdeploy.lock")

			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("configuration file %s already exists. Use --force to overwrite", configPath)
			}

			if err := os.MkdirAll(agentDir, 0755); err != nil {
				return fmt.Errorf("failed creating .agent directory: %w", err)
			}

			if appName == "" {
				wd, err := os.Getwd()
				if err == nil {
					appName = filepath.Base(wd)
				}
				if appName == "" || appName == "." {
					appName = "app"
				}
			}

			cfg := &contract.AppConfig{
				Name:  appName,
				Port:  appPort,
				Image: image,
			}

			if err := contract.Validate(cfg); err != nil {
				return fmt.Errorf("invalid configuration parameters: %w", err)
			}

			// Generate compact JSON manifest (< 10 lines)
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return fmt.Errorf("failed marshaling config template: %w", err)
			}
			data = append(data, '\n')

			if err := os.WriteFile(configPath, data, 0644); err != nil {
				return fmt.Errorf("failed writing %s: %w", configPath, err)
			}

			// Generate and write lockfile
			lock := contract.GenerateLockfile(cfg)
			if err := contract.WriteLockfile(lockPath, lock); err != nil {
				return fmt.Errorf("failed writing %s: %w", lockPath, err)
			}

			// Generate AI IDE bridge files
			if err := contract.GenerateBridgeFiles(".", cfg); err != nil {
				return fmt.Errorf("failed generating bridge files: %w", err)
			}

			cmd.Println("Initialized izDeploy workspace successfully:")
			cmd.Printf("  [+] Contract:  %s (%d lines)\n", configPath, len(splitLines(string(data))))
			cmd.Printf("  [+] Lockfile:  %s (infra_hash: %s)\n", lockPath, lock.InfraHash[:8])
			cmd.Printf("  [+] AI Bridge: %s\n", contract.CursorRulePath)
			cmd.Printf("  [+] AI Bridge: %s\n", contract.ClaudeGuidePath)
			return nil
		},
	}

	cmd.Flags().StringVar(&appName, "name", "app", "Application name (DNS-safe lowercase)")
	cmd.Flags().IntVar(&appPort, "port", 3000, "Internal application TCP port (1-65535)")
	cmd.Flags().StringVar(&image, "image", "ghcr.io/org/app:latest", "Target container image reference")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing configuration and lockfile")

	return cmd
}

func splitLines(s string) []string {
	var lines []string
	var line string
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, line)
			line = ""
		} else {
			line += string(r)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
