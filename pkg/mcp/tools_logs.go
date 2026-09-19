package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// DECISION: Enforce ring-buffer (15 head + 45 tail) with ANSI stripping for iz_logs.
// WHY: Protects LLM context window from massive log dumps (max 60 lines) while preserving
// both initial boot diagnostic context (head 15) and recent error terminations (tail 45).
// TRADE-OFF: Middle logs are summarized with a truncation marker.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-009

const (
	DefaultHeadLines = 15
	DefaultTailLines = 45
	MaxRingLines     = DefaultHeadLines + DefaultTailLines
)

// ansiRegex matches standard terminal escape codes including SGR colors and cursor movements.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b[()][AB012]`)

// StripANSI removes all ANSI escape sequences from input string.
//
// Business rule: Prevents raw terminal escape bytes from corrupting LLM markdown formatting.
//
// @ai-constraint: Pure string manipulation with zero side effects.
func StripANSI(input string) string {
	return ansiRegex.ReplaceAllString(input, "")
}

// RingBufferLogs filters a list of log lines into a compact head-and-tail ring buffer.
//
// Business rule: If lines <= 60, all lines are returned. Otherwise, returns 15 head + 45 tail.
//
// @ai-constraint: Strips ANSI formatting on every retained line.
func RingBufferLogs(rawLines []string, headCount int, tailCount int) string {
	if headCount <= 0 {
		headCount = DefaultHeadLines
	}
	if tailCount <= 0 {
		tailCount = DefaultTailLines
	}
	maxLines := headCount + tailCount

	var cleaned []string
	for _, l := range rawLines {
		cleanedLine := strings.TrimRight(StripANSI(l), "\r")
		if cleanedLine != "" {
			cleaned = append(cleaned, cleanedLine)
		}
	}

	total := len(cleaned)
	if total <= maxLines {
		return strings.Join(cleaned, "\n")
	}

	head := cleaned[:headCount]
	tail := cleaned[total-tailCount:]
	truncatedCount := total - maxLines

	return fmt.Sprintf("%s\n--- [truncated %d lines] ---\n%s",
		strings.Join(head, "\n"),
		truncatedCount,
		strings.Join(tail, "\n"),
	)
}

// registerLogsTool registers the iz_logs MCP tool on the server.
func (s *Server) registerLogsTool() {
	tool := mcp.NewTool("iz_logs",
		mcp.WithDescription("Retrieve application container logs filtered by a 60-line ring-buffer (15 head + 45 tail, ANSI stripped)"),
		mcp.WithNumber("tail",
			mcp.Description("Optional maximum lines to query from container runtime (default 100)"),
		),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tail := req.GetInt("tail", 100)

		logs, err := s.backend.GetLogs(ctx, tail)
		if err != nil {
			return FormatErrorResult(err), nil
		}

		return mcp.NewToolResultText(logs), nil
	})
}
