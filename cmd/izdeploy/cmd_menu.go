package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// DECISION: Provide an interactive terminal menu wizard for human operators with automatic TTY detection.
// WHY: Simplifies post-deploy configuration for humans without requiring memorization of complex CLI flags,
// while gracefully degrading to standard non-interactive help in AI agent / piped terminal environments.
// TRADE-OFF: Adds interactive prompt loop logic in a dedicated subcommand.
// REF: wiki/projects/izdeploy/izdeploy_roadmap_v0.0.4.md

// IsTerminalDevice determines whether the standard input stream is an interactive terminal.
func IsTerminalDevice() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func newMenuCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "menu",
		Short: "Launch interactive terminal management console (for human operators)",
		Long: `Interactive menu wizard providing quick access to deployment status, container logs,
database provisioning, secret management, and network tunnels.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !IsTerminalDevice() {
				cmd.Println("Non-interactive terminal detected. Run 'izdeploy --help' for CLI command reference.")
				return nil
			}

			reader := bufio.NewReader(os.Stdin)

			for {
				cmd.Println("\n==================================================")
				cmd.Println("        izDeploy Management Console (v0.1)        ")
				cmd.Println("==================================================")
				cmd.Println("1) Deployment Status & Metrics      (izdeploy status)")
				cmd.Println("2) Container Logs Inspection         (izdeploy logs)")
				cmd.Println("3) Manage Micro-Databases            (izdeploy db)")
				cmd.Println("4) Manage Secrets & Variables        (izdeploy secret)")
				cmd.Println("5) Network Tunnels & Hardening       (izdeploy tunnel)")
				cmd.Println("6) Backup Application Volumes        (izdeploy volume)")
				cmd.Println("7) Exit")
				cmd.Print("\nSelect an option [1-7]: ")

				input, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				choice := strings.TrimSpace(input)

				switch choice {
				case "1":
					cmd.Println("\n--- Executing: izdeploy status ---")
					statusCmd := newStatusCmd()
					_ = statusCmd.RunE(statusCmd, nil)

				case "2":
					cmd.Println("\n--- Executing: izdeploy logs ---")
					logsCmd := newLogsCmd()
					_ = logsCmd.RunE(logsCmd, nil)

				case "3":
					cmd.Println("\n--- Micro-Database Menu ---")
					cmd.Println("a) List managed databases  (izdeploy db list)")
					cmd.Println("b) Create MariaDB (96MB)   (izdeploy db create mariadb)")
					cmd.Println("c) Create PostgreSQL (128MB) (izdeploy db create postgres)")
					cmd.Println("d) Create Redis (32MB)     (izdeploy db create redis)")
					cmd.Print("Select DB option [a-d] or Enter to back: ")
					dbChoice, _ := reader.ReadString('\n')
					dbChoice = strings.TrimSpace(dbChoice)
					switch dbChoice {
					case "a":
						listCmd := newDBListCmd()
						_ = listCmd.RunE(listCmd, nil)
					case "b":
						createCmd := newDBCreateCmd()
						_ = createCmd.RunE(createCmd, []string{"mariadb"})
					case "c":
						createCmd := newDBCreateCmd()
						_ = createCmd.RunE(createCmd, []string{"postgres"})
					case "d":
						createCmd := newDBCreateCmd()
						_ = createCmd.RunE(createCmd, []string{"redis"})
					}

				case "4":
					cmd.Println("\n--- Secrets & Environment ---")
					cmd.Println("Usage: izdeploy secret set KEY=VALUE or --file .env")
					cmd.Print("Enter SECRET_KEY=VALUE to save (or Enter to cancel): ")
					secInput, _ := reader.ReadString('\n')
					secInput = strings.TrimSpace(secInput)
					if secInput != "" && strings.Contains(secInput, "=") {
						secCmd := newSecretCmd()
						_ = secCmd.Commands()[0].RunE(secCmd, []string{secInput})
					}

				case "5":
					cmd.Println("\n--- Network Tunnels & Hardening ---")
					statusTunnelCmd := newTunnelStatusCmd()
					_ = statusTunnelCmd.RunE(statusTunnelCmd, nil)
					cmd.Println("\nTo configure tunnels, run:")
					cmd.Println("  izdeploy tunnel setup cloudflare --token <token>")
					cmd.Println("  izdeploy tunnel setup tailscale --key <tskey-...>")

				case "6":
					cmd.Println("\n--- Volume Backup ---")
					cmd.Print("Enter volume name to backup (or Enter to cancel): ")
					volName, _ := reader.ReadString('\n')
					volName = strings.TrimSpace(volName)
					if volName != "" {
						volCmd := newVolumeCmd()
						_ = volCmd.Commands()[0].RunE(volCmd, []string{volName})
					}

				case "7", "q", "exit":
					cmd.Println("Exiting izDeploy management console.")
					return nil

				default:
					cmd.Println("Invalid selection. Please enter a number between 1 and 7.")
				}
			}

			return nil
		},
	}
}
