package diagnostics

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/izhubs/izdeploy/pkg/contract"
)

// DECISION: Classify errors via heuristic pattern matching on exit codes and stderr tokens.
// WHY: Decouples diagnostic layer from specific programming language runtimes (Node.js, Python, Go)
// while providing deterministic classification of code errors vs host infra failures.
// TRADE-OFF: Unseen error patterns default to conservative status.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-016

var (
	portClashRegex  = regexp.MustCompile(`(?i)(address already in use|bind: address already in use|port is already allocated)`)
	oomRegex        = regexp.MustCompile(`(?i)(out of memory|killed process|oom-killer|exit code 137)`)
	proxyRegex      = regexp.MustCompile(`(?i)(502 bad gateway|proxy_upstream|dial tcp.*refused)`)
	codeSyntaxRegex = regexp.MustCompile(`(?i)(syntaxerror|parse error|cannot find module|importerror|typeerror|referenceerror|panic:|fatal error:|traceback)`)
)

// AnalyzeExitCode evaluates container termination status and standard error output.
//
// Business rule: Exit 137 signals Linux cgroup OOM termination and is strictly non-actionable.
//
// @ai-constraint: Code bugs (syntax, missing imports, unhandled panics) set agent_actionable=true.
func AnalyzeExitCode(exitCode int, stderr string) ProblemDetails {
	cleanStderr := strings.TrimSpace(stderr)

	switch exitCode {
	case 137:
		// Standard Linux SIGKILL triggered by cgroup OOM killer
		detail := cleanStderr
		if detail == "" {
			detail = "Container exceeded memory allocation (cgroup OOM killer dispatched SIGKILL)."
		}
		return NewOOMProblem(detail)

	case 143:
		// SIGTERM (graceful shutdown requested by host)
		return NewProblem(
			UrnPrefix+"sigterm",
			"Container Terminated via SIGTERM",
			500,
			"Container received graceful shutdown signal from host orchestrator.",
			false,
			CodeInternal,
		)

	case 126, 127:
		// Command cannot be invoked or binary/interpreter missing in container
		detail := cleanStderr
		if detail == "" {
			detail = fmt.Sprintf("Container executable not found or permissions denied (exit %d).", exitCode)
		}
		return NewProblem(
			UrnPrefix+"executable-missing",
			"Entrypoint Execution Failed",
			500,
			detail,
			true, // Agent can adjust Dockerfile or start command
			CodeAppCrash,
		)

	default:
		// Inspect stderr patterns for common failure modes
		if portClashRegex.MatchString(cleanStderr) {
			return NewProblem(
				UrnPrefix+"port-conflict",
				"Network Port Clash",
				500,
				TruncateDetail(cleanStderr),
				false,
				CodePortConflict,
			)
		}

		if oomRegex.MatchString(cleanStderr) {
			return NewOOMProblem(cleanStderr)
		}

		if proxyRegex.MatchString(cleanStderr) {
			return NewProxyProblem(cleanStderr)
		}

		if codeSyntaxRegex.MatchString(cleanStderr) {
			return NewCodeProblem(cleanStderr)
		}

		// Fallback for general non-zero exit codes: treat as code-level crash
		detail := cleanStderr
		if detail == "" {
			detail = fmt.Sprintf("Application process exited unexpectedly with status %d.", exitCode)
		}
		return NewProblem(
			UrnPrefix+"app-crash",
			"Application Process Crashed",
			500,
			detail,
			true,
			CodeAppCrash,
		)
	}
}

// ClassifyError maps standard Go error types into RFC 7807 ProblemDetails.
//
// Business rule: Preserves existing ProblemDetails or unwraps contract validation errors.
//
// @ai-constraint: Never exposes sensitive host file paths or credentials in problem details.
func ClassifyError(err error) ProblemDetails {
	if err == nil {
		return ProblemDetails{}
	}

	var prob ProblemDetails
	if errors.As(err, &prob) {
		return prob
	}

	var syntaxErr *contract.SyntaxError
	if errors.As(err, &syntaxErr) {
		return NewConfigProblem(syntaxErr.Error())
	}

	var valErrs contract.ValidationErrors
	if errors.As(err, &valErrs) {
		return NewConfigProblem(valErrs.Error())
	}

	var valErr contract.ValidationError
	if errors.As(err, &valErr) {
		return NewConfigProblem(valErr.Error())
	}

	errMsg := err.Error()

	// Check lockfile mismatch
	if strings.Contains(errMsg, "infrastructure parameters modified without lockfile update") ||
		strings.Contains(errMsg, "lockfile not found") {
		return NewInfraLockedProblem(errMsg)
	}

	// Check port collision
	if portClashRegex.MatchString(errMsg) {
		return NewProblem(
			UrnPrefix+"port-conflict",
			"Network Port Clash",
			500,
			TruncateDetail(errMsg),
			false,
			CodePortConflict,
		)
	}

	// Check proxy errors
	if proxyRegex.MatchString(errMsg) {
		return NewProxyProblem(errMsg)
	}

	// Check OOM patterns
	if oomRegex.MatchString(errMsg) {
		return NewOOMProblem(errMsg)
	}

	// Default fallback
	return NewProblem(
		UrnPrefix+"runtime-error",
		"Runtime Execution Error",
		500,
		TruncateDetail(errMsg),
		false,
		CodeInternal,
	)
}
