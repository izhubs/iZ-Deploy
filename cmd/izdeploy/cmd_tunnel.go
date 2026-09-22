package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// DECISION: Provide native CLI commands for post-deployment network tunnel configuration (Cloudflare Tunnel & Tailscale SSH).
// WHY: Enables developers and AI agents to harden VPS security post-deployment, transitioning from public open ports to Zero-Inbound access.
// TRADE-OFF: Executes host systemctl/ufw commands which require sudo/root privileges on Linux hosts.
// REF: wiki/projects/izdeploy/izdeploy_roadmap_v0.0.4.md

func newTunnelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Manage secure outbound network tunnels (Cloudflare Tunnel & Tailscale SSH)",
		Long: `Configures secure network tunnels to eliminate public open ports on the VPS.
Supports Cloudflare Tunnel for ingress (closing ports 80/443) and Tailscale SSH for management (closing port 22).`,
	}

	cmd.AddCommand(newTunnelSetupCmd())
	cmd.AddCommand(newTunnelStatusCmd())

	return cmd
}

func newTunnelSetupCmd() *cobra.Command {
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup network tunnels (cloudflare or tailscale)",
	}

	setupCmd.AddCommand(newTunnelSetupCloudflareCmd())
	setupCmd.AddCommand(newTunnelSetupTailscaleCmd())

	return setupCmd
}

func newTunnelSetupCloudflareCmd() *cobra.Command {
	var (
		token  string
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "cloudflare",
		Short: "Setup Cloudflare Tunnel for secure ingress without open inbound ports",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return fmt.Errorf("cloudflare tunnel token is required (--token)")
			}

			plan := []string{
				"# 1. Install cloudflared systemd service",
				fmt.Sprintf("cloudflared service install %s", token),
				"systemctl daemon-reload",
				"systemctl enable --now cloudflared",
				"# 2. Lock down public web ports on UFW",
				"ufw delete allow 80/tcp || true",
				"ufw delete allow 443/tcp || true",
				"ufw reload",
			}

			if dryRun {
				cmd.Println("=== DRY RUN: Cloudflare Tunnel Provisioning Plan ===")
				for _, step := range plan {
					cmd.Println(step)
				}
				cmd.Println("\nAll inbound HTTP/HTTPS traffic will route through Cloudflare edge.")
				return nil
			}

			if runtime.GOOS != "linux" {
				return fmt.Errorf("tunnel setup is only supported on Linux VPS hosts (current OS: %s)", runtime.GOOS)
			}

			cmd.Println("Setting up Cloudflare Tunnel systemd service...")
			installCmd := exec.Command("cloudflared", "service", "install", token)
			installCmd.Stdout = os.Stdout
			installCmd.Stderr = os.Stderr
			if err := installCmd.Run(); err != nil {
				return fmt.Errorf("failed installing cloudflared service: %w", err)
			}

			_ = exec.Command("systemctl", "daemon-reload").Run()
			_ = exec.Command("systemctl", "enable", "--now", "cloudflared").Run()

			cmd.Println("Hardening firewall: removing public access to ports 80/443...")
			_ = exec.Command("ufw", "delete", "allow", "80/tcp").Run()
			_ = exec.Command("ufw", "delete", "allow", "443/tcp").Run()
			_ = exec.Command("ufw", "reload").Run()

			cmd.Println("Cloudflare Tunnel successfully configured. Ports 80 and 443 are now closed on public interface.")
			return nil
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "Cloudflare Tunnel service token")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview execution commands without modifying host")

	return cmd
}

func newTunnelSetupTailscaleCmd() *cobra.Command {
	var (
		key      string
		hostname string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "tailscale",
		Short: "Setup Tailscale Mesh & Tailscale SSH, closing public port 22",
		RunE: func(cmd *cobra.Command, args []string) error {
			if key == "" {
				return fmt.Errorf("tailscale auth key is required (--key)")
			}

			if hostname == "" {
				h, _ := os.Hostname()
				if h == "" {
					h = "iz-node"
				}
				hostname = fmt.Sprintf("iz-%s", h)
			}

			plan := []string{
				"# 1. Authenticate with Tailnet and enable Tailscale SSH",
				fmt.Sprintf("tailscale up --authkey=%s --ssh --hostname=%s --reset", key, hostname),
				"# 2. Restrict SSH to tailscale0 interface only",
				"ufw allow in on tailscale0 to any port 22 proto tcp comment 'izdeploy SSH via Tailscale only'",
				"ufw delete limit 22/tcp || true",
				"ufw delete allow 22/tcp || true",
				"ufw reload",
			}

			if dryRun {
				cmd.Println("=== DRY RUN: Tailscale SSH Provisioning Plan ===")
				for _, step := range plan {
					cmd.Println(step)
				}
				cmd.Println("\nPublic port 22 will be blocked. SSH access will be restricted to your Tailscale network.")
				return nil
			}

			if runtime.GOOS != "linux" {
				return fmt.Errorf("tunnel setup is only supported on Linux VPS hosts (current OS: %s)", runtime.GOOS)
			}

			cmd.Printf("Authenticating with Tailscale (hostname: %s)...\n", hostname)
			tsCmd := exec.Command("tailscale", "up", fmt.Sprintf("--authkey=%s", key), "--ssh", fmt.Sprintf("--hostname=%s", hostname), "--reset")
			tsCmd.Stdout = os.Stdout
			tsCmd.Stderr = os.Stderr
			if err := tsCmd.Run(); err != nil {
				return fmt.Errorf("failed configuring tailscale: %w", err)
			}

			cmd.Println("Hardening firewall: closing public port 22, permitting SSH only on tailscale0...")
			_ = exec.Command("ufw", "allow", "in", "on", "tailscale0", "to", "any", "port", "22", "proto", "tcp").Run()
			_ = exec.Command("ufw", "delete", "limit", "22/tcp").Run()
			_ = exec.Command("ufw", "delete", "allow", "22/tcp").Run()
			_ = exec.Command("ufw", "reload").Run()

			cmd.Printf("Tailscale SSH active. Connect via: ssh root@%s\n", hostname)
			return nil
		},
	}

	cmd.Flags().StringVar(&key, "key", "", "Tailscale pre-authenticated auth key (tskey-auth-...)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Machine hostname registered in Tailnet")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview execution commands without modifying host")

	return cmd
}

func newTunnelStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Inspect active status of network tunnels and firewall rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("=== izDeploy Network Security & Tunnel Status ===")

			if runtime.GOOS != "linux" {
				cmd.Printf("Operating System: %s (Local development environment)\n", runtime.GOOS)
				cmd.Println("Tunnel services and UFW firewall status checks are active on Linux nodes.")
				return nil
			}

			// Check Cloudflare Tunnel
			cmd.Println("\n[Cloudflare Tunnel Service]")
			cfStatus, err := exec.Command("systemctl", "is-active", "cloudflared").Output()
			if err == nil {
				cmd.Printf("  Status: %s", string(cfStatus))
			} else {
				cmd.Println("  Status: inactive or not installed")
			}

			// Check Tailscale
			cmd.Println("\n[Tailscale Status]")
			tsStatus, err := exec.Command("tailscale", "status").Output()
			if err == nil {
				lines := strings.Split(strings.TrimSpace(string(tsStatus)), "\n")
				if len(lines) > 0 {
					cmd.Printf("  %s\n", lines[0])
				}
			} else {
				cmd.Println("  Status: inactive or not installed")
			}

			// Check UFW
			cmd.Println("\n[Firewall Rules (UFW)]")
			ufwOut, err := exec.Command("ufw", "status").Output()
			if err == nil {
				cmd.Println(strings.TrimSpace(string(ufwOut)))
			} else {
				cmd.Println("  UFW status unavailable")
			}

			return nil
		},
	}
}
