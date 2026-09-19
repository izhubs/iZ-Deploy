package contract

import (
	"fmt"
	"os"
	"path/filepath"
)

// DECISION: Automatically project .agent/izdeploy.json contracts into .cursor/rules and CLAUDE.md.
// WHY: Injects hard boundaries directly into AI assistant context windows before token generation,
// reducing hallucinatory attempts to edit locked port allocations or delete proxy configurations.
// TRADE-OFF: Generates dual markdown bridge files that must be kept in sync with contract version.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-024

// CursorRulePath specifies standard relative path for Cursor IDE rule manifests.
const CursorRulePath = ".cursor/rules/izdeploy.mdc"

// ClaudeGuidePath specifies standard relative path for Claude Code workspace instruction.
const ClaudeGuidePath = "CLAUDE.md"

// WindsurfRulePath specifies standard relative path for Windsurf IDE rule manifests.
const WindsurfRulePath = ".windsurfrules"

// GenerateBridgeFiles creates .cursor/rules/izdeploy.mdc, CLAUDE.md, and .windsurfrules in target project directory.
//
// Business rule: Installs strict AI guardrails defining what is agent-actionable and what is infra-locked.
//
// @ai-constraint: Output files must not contain subjective fluff or conversational AI clichés.
func GenerateBridgeFiles(rootDir string, cfg *AppConfig) error {
	cursorPath := filepath.Join(rootDir, CursorRulePath)
	claudePath := filepath.Join(rootDir, ClaudeGuidePath)
	windsurfPath := filepath.Join(rootDir, WindsurfRulePath)

	cursorContent := generateCursorRuleContent(cfg)
	claudeContent := generateClaudeGuideContent(cfg)
	windsurfContent := generateWindsurfRuleContent(cfg)

	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		return fmt.Errorf("failed creating cursor rules directory: %w", err)
	}

	if err := os.WriteFile(cursorPath, []byte(cursorContent), 0644); err != nil {
		return fmt.Errorf("failed writing cursor rule %s: %w", cursorPath, err)
	}

	if err := os.WriteFile(claudePath, []byte(claudeContent), 0644); err != nil {
		return fmt.Errorf("failed writing claude guide %s: %w", claudePath, err)
	}

	if err := os.WriteFile(windsurfPath, []byte(windsurfContent), 0644); err != nil {
		return fmt.Errorf("failed writing windsurf rule %s: %w", windsurfPath, err)
	}

	return nil
}

// generateCursorRuleContent renders the Cursor MDC format with frontmatter.
func generateCursorRuleContent(cfg *AppConfig) string {
	appName := "app"
	appPort := 3000
	if cfg != nil {
		if cfg.Name != "" {
			appName = cfg.Name
		}
		if cfg.Port > 0 {
			appPort = cfg.Port
		}
	}

	return fmt.Sprintf(`---
description: izDeploy configuration governance and RFC 7807 error gating rules
globs: .agent/*, *.json, Dockerfile
alwaysApply: true
---

# izDeploy AI Contract Governance

Application: %s
Internal Port: %d
Configuration: .agent/izdeploy.json
Lockfile: .agent/izdeploy.lock

## Critical Guardrails
1. INFRASTRUCTURE LOCK:
   - DO NOT alter "port", "routes", "volumes", or "resources" in .agent/izdeploy.json.
   - Any modification to infrastructure fields requires explicit user consent and '--force'.
   - The lockfile .agent/izdeploy.lock verifies SHA-256 integrity during pre-deploy.

2. RFC 7807 DIAGNOSTICS:
   - When receiving an RFC 7807 Problem Details response:
     * If "agent_actionable" is true: inspect code, syntax, and build scripts to resolve the failure.
     * If "agent_actionable" is false: STOP. Do not modify infrastructure or local ports. Inform the developer.

3. WORKFLOW COMMANDS:
   - Validate config: 'izdeploy lint'
   - Check status: 'izdeploy status'
   - Deploy: 'izdeploy deploy'
`, appName, appPort)
}

// generateClaudeGuideContent renders standard CLAUDE.md instructions.
func generateClaudeGuideContent(cfg *AppConfig) string {
	appName := "app"
	appPort := 3000
	if cfg != nil {
		if cfg.Name != "" {
			appName = cfg.Name
		}
		if cfg.Port > 0 {
			appPort = cfg.Port
		}
	}

	return fmt.Sprintf(`# izDeploy Architecture & AI Contract

Application: %s
Configured Internal Port: %d

## Guidelines for AI Pair Programming
- Contract file: '.agent/izdeploy.json'
- Lockfile: '.agent/izdeploy.lock'

### Immutable Infrastructure Rules
- Never change internal container port (%d) or memory boundaries without developer instruction.
- Never edit or tamper with '.agent/izdeploy.lock' directly; use 'izdeploy lint' or 'izdeploy deploy --force'.

### Diagnostic Error Gating (RFC 7807)
- Errors returned by izDeploy CLI and MCP tools follow RFC 7807:
  - "agent_actionable: true" indicates application/code-level issues (syntax error, missing files). AI should fix.
  - "agent_actionable: false" indicates host-level issues (OOM 137, network port collision, proxy 502). AI must defer to user.
`, appName, appPort, appPort)
}

// generateWindsurfRuleContent renders standard .windsurfrules instructions.
func generateWindsurfRuleContent(cfg *AppConfig) string {
	appName := "app"
	appPort := 3000
	if cfg != nil {
		if cfg.Name != "" {
			appName = cfg.Name
		}
		if cfg.Port > 0 {
			appPort = cfg.Port
		}
	}

	return fmt.Sprintf(`# izDeploy Architecture & AI Contract (Windsurf)

Application: %s
Configured Internal Port: %d

## Guidelines for AI Pair Programming
- Contract file: .agent/izdeploy.json
- Lockfile: .agent/izdeploy.lock

### Immutable Infrastructure Rules
- Never change internal container port (%d) or memory boundaries without developer instruction.
- Never edit or tamper with .agent/izdeploy.lock directly; use izdeploy lint or izdeploy deploy --force.

### Diagnostic Error Gating (RFC 7807)
- Errors returned by izDeploy CLI and MCP tools follow RFC 7807:
  - "agent_actionable: true" indicates application/code-level issues (syntax error, missing files). AI should fix.
  - "agent_actionable: false" indicates host-level issues (OOM 137, network port collision, proxy 502). AI must defer to user.
`, appName, appPort, appPort)
}