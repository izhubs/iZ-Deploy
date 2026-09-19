package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// DECISION: Accept both object map and single key-value inputs in iz_env.
// WHY: Different LLM clients invoke tool schemas with either dictionary maps or individual strings.
// Supporting both eliminates invocation friction.
// TRADE-OFF: Additional type detection logic in tool handler.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

// registerEnvTool registers the iz_env MCP tool on the server.
func (s *Server) registerEnvTool() {
	tool := mcp.NewTool("iz_env",
		mcp.WithDescription("Safely update runtime environment variables in .agent/izdeploy.json and trigger graceful container reload"),
		mcp.WithString("key",
			mcp.Description("Single environment variable key name (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)"),
		),
		mcp.WithString("value",
			mcp.Description("Single environment variable value"),
		),
	)

	s.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		vars := make(map[string]string)

		// Check single key-value
		key := req.GetString("key", "")
		val := req.GetString("value", "")
		if key != "" {
			vars[key] = val
		}

		// Also check raw map argument "variables" if client supplied an object
		args := req.GetArguments()
		if rawVars, ok := args["variables"]; ok {
			if varMap, ok := rawVars.(map[string]any); ok {
				for k, v := range varMap {
					vars[k] = fmt.Sprint(v)
				}
			}
		}

		if len(vars) == 0 {
			return FormatErrorResult(errors.New("at least one environment variable (key/value or variables map) must be provided")), nil
		}

		res, err := s.backend.SetEnv(ctx, vars)
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
