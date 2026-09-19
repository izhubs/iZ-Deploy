// Package main manages host firewall break-glass rule lifecycle.
package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Firewall rule identifiers and command constants.
const (
	BreakGlassComment = "izdeploy-break-glass"
	UfwBinaryName     = "ufw"
	Port22LimitRule   = "22/tcp"
)

// DECISION: Inject 'ufw limit 22/tcp' rather than 'ufw allow 22/tcp' during break-glass mode.
// WHY: ufw limit enforces rate limiting (maximum 6 connection attempts per 30 seconds per source IP),
// actively thwarting automated SSH credential brute-forcing while port 22 is temporarily exposed.
// TRADE-OFF: Operators establishing rapid repetitive SSH sessions may experience throttling.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-017

// FirewallController abstracts packet filter rule manipulation.
type FirewallController interface {
	EnableBreakGlass() error
	DisableBreakGlass() error
	IsBreakGlassActive() (bool, error)
}

// UFWFirewall executes host iptables/UFW CLI invocations.
type UFWFirewall struct{}

// EnableBreakGlass injects a rate-limited SSH rule at index 1 with comment tracking.
//
// Business rule: Rule must be inserted at position 1 to supersede any general drop rules.
//
// @ai-constraint: Must use exec.Command with separated arguments to prevent shell injection.
func (u *UFWFirewall) EnableBreakGlass() error {
	cmd := exec.Command(UfwBinaryName, "insert", "1", "limit", Port22LimitRule, "comment", BreakGlassComment)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw enable break-glass failed: %w (output: %s)", err, strings.TrimSpace(string(outputBytes)))
	}
	return nil
}

// DisableBreakGlass revokes the emergency SSH opening rule.
//
// Business rule: Rule revocation must cleanly remove the rate-limited 22 rule.
func (u *UFWFirewall) DisableBreakGlass() error {
	cmd := exec.Command(UfwBinaryName, "delete", "limit", Port22LimitRule)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw disable break-glass failed: %w (output: %s)", err, strings.TrimSpace(string(outputBytes)))
	}
	return nil
}

// IsBreakGlassActive inspects UFW status output for presence of the break-glass marker.
func (u *UFWFirewall) IsBreakGlassActive() (bool, error) {
	cmd := exec.Command(UfwBinaryName, "status")
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("ufw status inspection failed: %w", err)
	}
	return strings.Contains(string(outputBytes), BreakGlassComment), nil
}

// MockFirewall provides in-memory firewall state tracking for unit testing and dry-run execution.
type MockFirewall struct {
	isActive    bool
	enableCount int
	deleteCount int
	mutex       sync.Mutex
}

// EnableBreakGlass tracks mock rule insertion.
func (m *MockFirewall) EnableBreakGlass() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.isActive = true
	m.enableCount++
	return nil
}

// DisableBreakGlass tracks mock rule removal.
func (m *MockFirewall) DisableBreakGlass() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.isActive = false
	m.deleteCount++
	return nil
}

// IsBreakGlassActive returns the mock firewall status.
func (m *MockFirewall) IsBreakGlassActive() (bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.isActive, nil
}
