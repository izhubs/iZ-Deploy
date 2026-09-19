package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// DECISION: Support configurable grace period with 10s default for iz_restart.
// WHY: Gives web applications sufficient time to drain in-flight HTTP connections
// before issuing hard SIGKILL termination.
// TRADE-OFF: Command blocks for up to grace_period_seconds during active drain.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

// registerRestartTool registers the iz_restart MCP tool on the server.
func (s *Server) registerRestartTool() {
	tool := mcp.NewTool("iz_restart",
		mcp.WithDescription("Gracefully restart application container with connection draining and 10s SIGKILL timeout"),
		mcp.WithNumber("grace_period_seconds",
			mcp.Description("Optional seconds to wait for container to stop before SIGKILL (default 10)"),
		),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		gracePeriod := req.GetInt("grace_period_seconds", 10)

		res, err := s.backend.Restart(ctx, gracePeriod)
		if err != nil {
			return FormatErrorResult(err), nil
		}

		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return FormatErrorResult(err), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	})
}
