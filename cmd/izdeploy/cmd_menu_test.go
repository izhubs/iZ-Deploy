package main

import (
	"testing"
)

func TestIsTerminalDevice(t *testing.T) {
	// Must execute without panic
	_ = IsTerminalDevice()
}

func TestNewMenuCmd_Structure(t *testing.T) {
	cmd := newMenuCmd()
	if cmd.Use != "menu" {
		t.Errorf("expected Use 'menu', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short description for menu")
	}
}
