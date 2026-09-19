package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// DECISION: Register iz_status as zero-parameter introspection tool.
// WHY: Gives AI instant visibility into app runtime status, RAM usage, and uptime
// without requiring SSH or container shell access.
// TRADE-OFF: None.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

// registerStatusTool registers the iz_status MCP tool on the server.
func (s *Server) registerStatusTool() {
	tool := mcp.NewTool("iz_status",
		mcp.WithDescription("Query application status, container health, uptime, RAM MB, and CPU percentage"),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status, err := s.backend.GetStatus(ctx)
		if err != nil {
			return FormatErrorResult(err), nil
		}

		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return FormatErrorResult(err), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	})
}
