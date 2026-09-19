// Package docker provides an enterprise-grade Go SDK wrapper around the Docker Engine API.
package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
)

// Standard lifecycle constants.
const (
	DefaultGracePeriodSeconds = 10
	DefaultImagePullTimeout   = 10 * time.Minute
	BytesPerMegabyte          = 1024 * 1024
	NanoCPUsPerCore           = 1e9
)

// Container exit error classifications.
var (
	ErrContainerOOM      = errors.New("container terminated by out-of-memory killer (exit 137)")
	ErrContainerCrash    = errors.New("container process exited with fatal error (exit 1)")
	ErrContainerNotFound = errors.New("container not found")
)

// DeploySpec specifies the runtime parameters for provisioning an application container.
//
// Business rule: MemoryLimitMB enforces cgroups v2 memory boundary to prevent node OOM cascade.
type DeploySpec struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Port          int               `json:"port"`
	HostIP        string            `json:"host_ip"`
	Env           map[string]string `json:"env"`
	Volumes       map[string]string `json:"volumes"`
	MemoryLimitMB int64             `json:"memory_limit_mb"`
	CPULimit      float64           `json:"cpu_limit"`
	Labels        map[string]string `json:"labels"`
}

// ContainerStatus captures the current execution state of an inspected container.
type ContainerStatus struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	State     string    `json:"state"`
	Status    string    `json:"status"`
	ExitCode  int       `json:"exit_code"`
	OOMKilled bool      `json:"oom_killed"`
	CreatedAt time.Time `json:"created_at"`
	StartedAt time.Time `json:"started_at"`
}

// DECISION: Graceful shutdown with 10s SIGTERM before kernel SIGKILL.
// WHY: Gives web applications sufficient time to drain connections and complete DB writes.
// TRADE-OFF: Deployment swap takes up to 10s when container refuses to terminate on SIGTERM.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-005

// PullImage pulls the specified container image and drains output to completion.
//
// Business rule: Image pull must finish or timeout cleanly without abandoning open socket readers.
//
// @ai-constraint: Always drain the reader to io.Discard; unread streams stall Docker daemon workers.
func (c *Client) PullImage(ctx context.Context, imageRef string) error {
	pullCtx, cancel := context.WithTimeout(ctx, DefaultImagePullTimeout)
	defer cancel()

	reader, err := c.cli.ImagePull(pullCtx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed pulling image %s: %w", imageRef, err)
	}
	defer func() { _ = reader.Close() }()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("failed streaming image pull progress: %w", err)
	}
	return nil
}

// DeployContainer replaces or launches an application container with configured cgroups v2 limits.
//
// Business rule: Any pre-existing container with matching name must be cleanly stopped and removed.
func (c *Client) DeployContainer(ctx context.Context, spec DeploySpec) (string, error) {
	if err := c.cleanupExistingContainer(ctx, spec.Name); err != nil {
		return "", err
	}

	containerConfig, hostConfig, err := c.buildContainerConfigs(spec)
	if err != nil {
		return "", err
	}

	createResp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("failed creating container %s: %w", spec.Name, err)
	}

	if err := c.cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		return createResp.ID, fmt.Errorf("failed starting container %s: %w", spec.Name, err)
	}

	return createResp.ID, nil
}

// StopContainer requests graceful SIGTERM termination with timeout fallback to SIGKILL.
func (c *Client) StopContainer(ctx context.Context, containerID string, graceSeconds int) error {
	if graceSeconds <= 0 {
		graceSeconds = DefaultGracePeriodSeconds
	}
	stopOpts := container.StopOptions{Timeout: &graceSeconds}
	if err := c.cli.ContainerStop(ctx, containerID, stopOpts); err != nil {
		return fmt.Errorf("failed stopping container %s: %w", containerID, err)
	}
	return nil
}

// RemoveContainer deletes an existing container from local daemon storage.
func (c *Client) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	removeOpts := container.RemoveOptions{Force: force, RemoveVolumes: true}
	if err := c.cli.ContainerRemove(ctx, containerID, removeOpts); err != nil {
		return fmt.Errorf("failed removing container %s: %w", containerID, err)
	}
	return nil
}

// RestartContainer restarts an active or stopped container within the specified grace period.
func (c *Client) RestartContainer(ctx context.Context, containerID string, graceSeconds int) error {
	if graceSeconds <= 0 {
		graceSeconds = DefaultGracePeriodSeconds
	}
	stopOpts := container.StopOptions{Timeout: &graceSeconds}
	if err := c.cli.ContainerRestart(ctx, containerID, stopOpts); err != nil {
		return fmt.Errorf("failed restarting container %s: %w", containerID, err)
	}
	return nil
}

// InspectContainer inspects runtime execution state and detects OOM or exit errors.
func (c *Client) InspectContainer(ctx context.Context, containerID string) (*ContainerStatus, error) {
	inspectJSON, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting container %s: %w", containerID, err)
	}

	parsedCreatedAt, _ := time.Parse(time.RFC3339Nano, inspectJSON.Created)
	var parsedStartedAt time.Time
	if inspectJSON.State != nil {
		parsedStartedAt, _ = time.Parse(time.RFC3339Nano, inspectJSON.State.StartedAt)
	}

	status := &ContainerStatus{
		ID:        inspectJSON.ID,
		Name:      strings.TrimPrefix(inspectJSON.Name, "/"),
		Image:     inspectJSON.Config.Image,
		CreatedAt: parsedCreatedAt,
		StartedAt: parsedStartedAt,
	}

	if inspectJSON.State != nil {
		status.State = inspectJSON.State.Status
		status.Status = inspectJSON.State.Status
		status.ExitCode = inspectJSON.State.ExitCode
		status.OOMKilled = inspectJSON.State.OOMKilled
	}

	return status, nil
}

// FindContainerByName locates a container by its exact name match.
func (c *Client) FindContainerByName(ctx context.Context, name string) (*ContainerStatus, error) {
	exactNameFilter := fmt.Sprintf("^/%s$", strings.TrimPrefix(name, "/"))
	containersList, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", exactNameFilter)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed searching container by name: %w", err)
	}
	if len(containersList) == 0 {
		return nil, ErrContainerNotFound
	}
	return c.InspectContainer(ctx, containersList[0].ID)
}

func (c *Client) cleanupExistingContainer(ctx context.Context, name string) error {
	existing, err := c.FindContainerByName(ctx, name)
	if err != nil {
		if errors.Is(err, ErrContainerNotFound) {
			return nil
		}
		return err
	}

	_ = c.StopContainer(ctx, existing.ID, DefaultGracePeriodSeconds)
	return c.RemoveContainer(ctx, existing.ID, true)
}

// BuildContainerConfigs constructs Docker container and host configuration objects from DeploySpec.
//
// Business rule: Translates high-level spec fields into low-level Docker cgroups v2, port, and volume bindings.
func BuildContainerConfigs(spec DeploySpec) (*container.Config, *container.HostConfig, error) {
	envList := make([]string, 0, len(spec.Env))
	for key, val := range spec.Env {
		envList = append(envList, fmt.Sprintf("%s=%s", key, val))
	}

	portProto := nat.Port(fmt.Sprintf("%d/tcp", spec.Port))
	hostIP := spec.HostIP
	if hostIP == "" {
		hostIP = "127.0.0.1"
	}

	containerConfig := &container.Config{
		Image:        spec.Image,
		Env:          envList,
		Labels:       spec.Labels,
		ExposedPorts: nat.PortSet{portProto: struct{}{}},
	}

	binds := make([]string, 0, len(spec.Volumes))
	for hostPath, contPath := range spec.Volumes {
		binds = append(binds, fmt.Sprintf("%s:%s:rw", hostPath, contPath))
	}

	var memoryBytes int64
	if spec.MemoryLimitMB > 0 {
		memoryBytes = spec.MemoryLimitMB * BytesPerMegabyte
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			portProto: []nat.PortBinding{{HostIP: hostIP, HostPort: fmt.Sprintf("%d", spec.Port)}},
		},
		Binds: binds,
		Resources: container.Resources{
			Memory:   memoryBytes,
			NanoCPUs: int64(spec.CPULimit * NanoCPUsPerCore),
		},
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	return containerConfig, hostConfig, nil
}

func (c *Client) buildContainerConfigs(spec DeploySpec) (*container.Config, *container.HostConfig, error) {
	return BuildContainerConfigs(spec)
}
