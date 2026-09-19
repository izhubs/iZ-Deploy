package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// DECISION: Block deployment in iz_deploy if lockfile verification fails without explicit force flag.
// WHY: Enforces infrastructure protection boundary directly inside the MCP tool invocation layer.
// TRADE-OFF: Developer must explicitly pass force=true to bypass lockfile during legitimate refactors.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

// registerDeployTool registers the iz_deploy MCP tool on the server.
func (s *Server) registerDeployTool() {
	tool := mcp.NewTool("iz_deploy",
		mcp.WithDescription("Trigger deployment of a new container image tag with zero-downtime swap and pre-deploy lock verification"),
		mcp.WithString("image_tag",
			mcp.Required(),
			mcp.Description("Target container image tag reference (e.g. ghcr.io/org/app:v1.2.3)"),
		),
		mcp.WithBoolean("force",
			mcp.Description("Bypass infrastructure lockfile verification check if configuration has changed"),
		),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		imageTag, err := req.RequireString("image_tag")
		if err != nil || strings.TrimSpace(imageTag) == "" {
			return FormatErrorResult(err), nil
		}

		force := req.GetBool("force", false)

		res, err := s.backend.Deploy(ctx, imageTag, force)
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
