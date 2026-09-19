// Package mcp implements the Model Context Protocol stdio server and tools for izDeploy.
package mcp

import (
	"context"
	"os"

	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// DECISION: Embed standard 5-tool MCP catalog directly into izdeploy binary via stdio.
// WHY: Allows zero-configuration plug-and-play integration with Cursor, Claude Code, and Windsurf
// without requiring separate network listeners or external runtime orchestrators.
// TRADE-OFF: Tool handlers must operate with strict timeouts to avoid blocking stdio transport.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

// Server wraps the mcp-go server instance and execution backend.
type Server struct {
	mcpServer *server.MCPServer
	backend   DeploymentBackend
}

// ServerOptions configures the initialization parameters for the MCP server.
type ServerOptions struct {
	Name       string
	Version    string
	ProjectDir string
	Backend    DeploymentBackend
}

// NewServer initializes a new izDeploy MCP server with all 5 operational tools registered.
//
// Business rule: Tools enforce RFC 7807 error gating on any failure.
//
// @ai-constraint: Never log diagnostic messages to stdout because stdout is reserved for JSON-RPC.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Name == "" {
		opts.Name = "izdeploy"
	}
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.ProjectDir == "" {
		opts.ProjectDir = "."
	}
	if opts.Backend == nil {
		opts.Backend = NewLocalBackend(opts.ProjectDir)
	}

	s := server.NewMCPServer(
		opts.Name,
		opts.Version,
		server.WithToolCapabilities(true),
	)

	srv := &Server{
		mcpServer: s,
		backend:   opts.Backend,
	}

	// Register the 5 core tools
	srv.registerStatusTool()
	srv.registerDeployTool()
	srv.registerLogsTool()
	srv.registerRestartTool()
	srv.registerEnvTool()

	return srv, nil
}

// FormatErrorResult converts any Go error into an RFC 7807 ProblemDetails JSON error result.
//
// Business rule: All tool errors MUST return RFC 7807 Problem Details to enforce agent gating.
//
// @ai-constraint: Sets IsError=true on the CallToolResult to notify MCP clients of failure.
func FormatErrorResult(err error) *mcp.CallToolResult {
	prob := diagnostics.ClassifyError(err)
	return mcp.NewToolResultError(prob.Error())
}

// ServeStdio launches the stdio transport listener loop for the MCP server.
//
// Business rule: Terminates gracefully when context is canceled or stdin reaches EOF.
//
// @ai-constraint: Must redirect all internal logging to stderr to prevent stdio protocol corruption.
func (s *Server) ServeStdio(ctx context.Context) error {
	stdioServer := server.NewStdioServer(s.mcpServer)
	return stdioServer.Listen(ctx, os.Stdin, os.Stdout)
}

// MCPServer returns the underlying server instance for test inspection.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcpServer
}
