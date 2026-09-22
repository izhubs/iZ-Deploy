package main

import (
	"strings"
	"testing"
)

func TestNewTunnelCmd_Structure(t *testing.T) {
	cmd := newTunnelCmd()
	if cmd.Use != "tunnel" {
		t.Errorf("expected Use 'tunnel', got %q", cmd.Use)
	}

	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	if !subcommands["setup"] {
		t.Error("expected subcommand 'setup' under 'tunnel'")
	}
	if !subcommands["status"] {
		t.Error("expected subcommand 'status' under 'tunnel'")
	}
}

func TestNewTunnelSetupCmd_Subcommands(t *testing.T) {
	cmd := newTunnelSetupCmd()
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	if !subcommands["cloudflare"] {
		t.Error("expected 'cloudflare' under 'tunnel setup'")
	}
	if !subcommands["tailscale"] {
		t.Error("expected 'tailscale' under 'tunnel setup'")
	}
}

func TestTunnelSetupCloudflare_MissingToken(t *testing.T) {
	cmd := newTunnelSetupCloudflareCmd()
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "token is required") {
		t.Errorf("expected missing token error, got: %v", err)
	}
}

func TestTunnelSetupTailscale_MissingKey(t *testing.T) {
	cmd := newTunnelSetupTailscaleCmd()
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "auth key is required") {
		t.Errorf("expected missing auth key error, got: %v", err)
	}
}

func TestTunnelSetupCloudflare_DryRun(t *testing.T) {
	cmd := newTunnelSetupCloudflareCmd()
	_ = cmd.Flags().Set("token", "dummy-cf-token-12345")
	_ = cmd.Flags().Set("dry-run", "true")

	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error in dry-run mode: %v", err)
	}
}

func TestTunnelSetupTailscale_DryRun(t *testing.T) {
	cmd := newTunnelSetupTailscaleCmd()
	_ = cmd.Flags().Set("key", "tskey-auth-dummy-67890")
	_ = cmd.Flags().Set("dry-run", "true")

	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error in dry-run mode: %v", err)
	}
}
