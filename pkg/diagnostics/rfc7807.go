// Package diagnostics implements RFC 7807 Problem Details serialization and error classification
// for izDeploy CLI, daemon, and MCP tools to enforce AI agent-actionable boundaries.
package diagnostics

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DECISION: Adopt RFC 7807 Problem Details JSON format as universal diagnostic protocol.
// WHY: Provides structured machine-readable error semantics enabling AI agents to distinguish
// between code-level repairable bugs and forbidden host-level infrastructure mutations.
// TRADE-OFF: All runtime errors must be wrapped into ProblemDetails before output.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-016

// Maximum detail character budget to prevent LLM context exhaustion.
const MaxDetailLength = 500

// Standard URN prefix for izDeploy diagnostic problems.
const UrnPrefix = "urn:izdeploy:error:"

// Error code identifiers.
const (
	CodeConfigInvalid     = "ERR_CONFIG_INVALID"
	CodeInfraLocked       = "ERR_INFRA_LOCKED"
	CodeContainerOOM      = "ERR_CONTAINER_OOM"
	CodePortConflict      = "ERR_PORT_CONFLICT"
	CodeProxyUpstream     = "ERR_PROXY_UPSTREAM"
	CodeAppCrash          = "ERR_APP_CRASH"
	CodeSyntaxError       = "ERR_SYNTAX_ERROR"
	CodeDeploymentFailure = "ERR_DEPLOY_FAILURE"
	CodeInternal          = "ERR_INTERNAL"
)

// ProblemDetails represents an RFC 7807 Problem Details structure extended with agent gating.
//
// Business rule: agent_actionable determines if AI is authorized to attempt autonomous code fixes.
//
// @ai-constraint: detail string is strictly capped at MaxDetailLength characters.
type ProblemDetails struct {
	Type            string `json:"type"`
	Title           string `json:"title"`
	Status          int    `json:"status"`
	Detail          string `json:"detail"`
	Instance        string `json:"instance,omitempty"`
	AgentActionable bool   `json:"agent_actionable"`
	ErrorCode       string `json:"error_code,omitempty"`
}

// Error formats ProblemDetails into an RFC 7807 compliant JSON string representation.
func (p ProblemDetails) Error() string {
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Sprintf("[%s] %s: %s (agent_actionable: %v)", p.ErrorCode, p.Title, p.Detail, p.AgentActionable)
	}
	return string(b)
}

// JSON returns indented JSON bytes for terminal and MCP tool presentation.
func (p ProblemDetails) JSON() []byte {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return []byte(p.Error())
	}
	return b
}

// TruncateDetail ensures detail string does not exceed token allocation limits.
//
// Business rule: Long stderr stack traces must be truncated gracefully.
//
// @ai-constraint: Never exceed MaxDetailLength (500 characters).
func TruncateDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len(detail) <= MaxDetailLength {
		return detail
	}
	return detail[:MaxDetailLength-3] + "..."
}

// NewProblem creates a customized ProblemDetails instance with sanitized detail limits.
func NewProblem(problemType string, title string, status int, detail string, actionable bool, code string) ProblemDetails {
	return ProblemDetails{
		Type:            problemType,
		Title:           title,
		Status:          status,
		Detail:          TruncateDetail(detail),
		AgentActionable: actionable,
		ErrorCode:       code,
	}
}

// NewConfigProblem creates a 400 ProblemDetails for contract schema and validation failures.
func NewConfigProblem(detail string) ProblemDetails {
	return NewProblem(
		UrnPrefix+"config-invalid",
		"Invalid Application Configuration",
		400,
		detail,
		true, // Agent can correct .agent/izdeploy.json syntax
		CodeConfigInvalid,
	)
}

// NewInfraLockedProblem creates a 422 ProblemDetails when infrastructure parameters are locked.
func NewInfraLockedProblem(detail string) ProblemDetails {
	return NewProblem(
		UrnPrefix+"infra-locked",
		"Infrastructure Modification Blocked",
		422,
		detail,
		false, // Agent must NOT unilaterally mutate locked host infrastructure
		CodeInfraLocked,
	)
}

// NewOOMProblem creates a 500 ProblemDetails for Linux cgroup exit code 137.
func NewOOMProblem(detail string) ProblemDetails {
	return NewProblem(
		UrnPrefix+"oom-killed",
		"Container Out of Memory (OOM 137)",
		500,
		detail,
		false, // Infra-level capacity limit; agent must NOT randomly change host cgroups
		CodeContainerOOM,
	)
}

// NewPortConflictProblem creates a 500 ProblemDetails for host TCP bind collisions.
func NewPortConflictProblem(port int, detail string) ProblemDetails {
	msg := fmt.Sprintf("TCP port %d is already in use by another service on this host. %s", port, detail)
	return NewProblem(
		UrnPrefix+"port-conflict",
		"Network Port Clash",
		500,
		msg,
		false, // Agent must NOT unilaterally alter reserved port bindings
		CodePortConflict,
	)
}

// NewProxyProblem creates a 502 ProblemDetails for reverse proxy upstream connection errors.
func NewProxyProblem(detail string) ProblemDetails {
	return NewProblem(
		UrnPrefix+"proxy-upstream",
		"Reverse Proxy Upstream Unavailable",
		502,
		detail,
		false, // Infra routing issue
		CodeProxyUpstream,
	)
}

// NewCodeProblem creates a 500 ProblemDetails for application-level runtime bugs or crashes.
func NewCodeProblem(detail string) ProblemDetails {
	return NewProblem(
		UrnPrefix+"code-runtime",
		"Application Code Runtime Error",
		500,
		detail,
		true, // Agent is explicitly encouraged to diagnose source code and repair
		CodeAppCrash,
	)
}
