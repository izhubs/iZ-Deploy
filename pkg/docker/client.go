// Package docker provides an enterprise-grade Go SDK wrapper around the Docker Engine API.
package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/client"
)

// DefaultDockerSocket specifies the standard Unix domain socket path for Docker daemon communication.
const DefaultDockerSocket = "unix:///var/run/docker.sock"

// DefaultPingTimeoutSeconds specifies the timeout deadline for engine health checks.
const DefaultPingTimeoutSeconds = 5

// DECISION: Wrap official Docker client SDK with explicit API version negotiation.
// WHY: Ensures compatibility across diverse Docker daemon versions without hardcoded REST versions.
// TRADE-OFF: Requires a round-trip ping handshake during client initialization.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-005

// Client provides controlled lifecycle access to the host Docker daemon.
//
// Business rule: All container operations must be executed through this client to guarantee
// context propagation, timeout enforcement, and uniform error classification.
type Client struct {
	cli *client.Client
}

// NewClient initializes a negotiated Docker API client.
//
// Business rule: If hostAddress is empty, configuration defaults to standard environment variables
// and local unix domain socket.
//
// @ai-constraint: Never disable API version negotiation; daemon version mismatch produces cryptic HTTP 400 errors.
func NewClient(hostAddress string) (*Client, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}

	if hostAddress != "" {
		opts = append(opts, client.WithHost(hostAddress))
	} else {
		opts = append(opts, client.FromEnv)
	}

	dockerCli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed creating docker client: %w", err)
	}

	return &Client{cli: dockerCli}, nil
}

// Ping verifies active connectivity to the Docker daemon.
//
// Business rule: Health check must fail fast if the daemon process is hung or socket permissions are denied.
func (c *Client) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, DefaultPingTimeoutSeconds*time.Second)
	defer cancel()
	_, err := c.cli.Ping(pingCtx)
	if err != nil {
		return fmt.Errorf("docker ping failed: %w", err)
	}
	return nil
}

// RawClient provides access to the underlying Docker client for advanced testing and mocking.
func (c *Client) RawClient() *client.Client {
	return c.cli
}

// Close terminates open connections to the Docker daemon socket.
func (c *Client) Close() error {
	if c.cli == nil {
		return nil
	}
	return c.cli.Close()
}
