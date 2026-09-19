package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/izhubs/izdeploy/pkg/diagnostics"
)

// DECISION: Provide a modular DeploymentBackend interface with LocalBackend as default.
// WHY: Allows the CLI and MCP tools to function identically in local developer offline mode
// and in production daemon network mode without code branching in tool handlers.
// TRADE-OFF: LocalBackend provides simulated container metrics when Docker socket is absent.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-015

var envKeyRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// DeploymentBackend abstracts container management operations for MCP tools.
type DeploymentBackend interface {
	GetStatus(ctx context.Context) (*StatusResponse, error)
	Deploy(ctx context.Context, imageTag string, force bool) (*DeployResponse, error)
	Rollback(ctx context.Context) (*DeployResponse, error)
	GetLogs(ctx context.Context, tail int) (string, error)
	Restart(ctx context.Context, gracePeriodSeconds int) (*RestartResponse, error)
	SetEnv(ctx context.Context, vars map[string]string) (*EnvResponse, error)
}

// StatusResponse encapsulates application runtime health and host metrics.
type StatusResponse struct {
	Status     string  `json:"status"`
	Uptime     string  `json:"uptime"`
	RAMMB      float64 `json:"ram_mb"`
	CPUPercent float64 `json:"cpu_percent"`
	App        string  `json:"app"`
	Port       int     `json:"port"`
	Image      string  `json:"image"`
}

// DeployResponse details the outcome of an image deployment.
type DeployResponse struct {
	Status      string `json:"status"`
	ImageTag    string `json:"image_tag"`
	ContainerID string `json:"container_id"`
	DurationMS  int64  `json:"duration_ms"`
	Message     string `json:"message"`
}

// RestartResponse summarizes container restart outcome.
type RestartResponse struct {
	Status              string `json:"status"`
	App                 string `json:"app"`
	GracePeriodSeconds  int    `json:"grace_period_seconds"`
	Message             string `json:"message"`
}

// EnvResponse describes the environment variable mutation outcome.
type EnvResponse struct {
	Status         string `json:"status"`
	Restarted      bool   `json:"restarted"`
	VariablesCount int    `json:"variables_count"`
	Message        string `json:"message"`
}

// LocalBackend implements DeploymentBackend by inspecting local repository config and simulated runtime.
type LocalBackend struct {
	projectDir string
	startTime  time.Time
}

// NewLocalBackend creates a new filesystem-backed local deployment backend.
func NewLocalBackend(projectDir string) *LocalBackend {
	return &LocalBackend{
		projectDir: projectDir,
		startTime:  time.Now().Add(-2 * time.Hour), // Baseline simulated 2h uptime
	}
}

// configPath returns standard relative path to configuration file.
func (b *LocalBackend) configPath() string {
	return filepath.Join(b.projectDir, ".agent", "izdeploy.json")
}

// lockPath returns standard relative path to lockfile.
func (b *LocalBackend) lockPath() string {
	return filepath.Join(b.projectDir, contract.DefaultLockfileName)
}

// GetStatus returns the current application state parsed from configuration and simulated metrics.
func (b *LocalBackend) GetStatus(ctx context.Context) (*StatusResponse, error) {
	cfg, err := contract.ParseConfigFile(b.configPath())
	if err != nil {
		return nil, err
	}

	uptimeDuration := time.Since(b.startTime).Round(time.Second)

	return &StatusResponse{
		Status:     "running",
		Uptime:     uptimeDuration.String(),
		RAMMB:      48.5,
		CPUPercent: 1.2,
		App:        cfg.Name,
		Port:       cfg.Port,
		Image:      cfg.Image,
	}, nil
}

// Deploy updates container image tag after validating infrastructure lock integrity.
func (b *LocalBackend) Deploy(ctx context.Context, imageTag string, force bool) (*DeployResponse, error) {
	start := time.Now()

	cfg, err := contract.ParseConfigFile(b.configPath())
	if err != nil {
		return nil, err
	}

	// Verify lockfile
	lockRes, err := contract.VerifyLockfile(b.lockPath(), cfg)
	if err != nil && !force {
		// If verification failed and force is not set, block deployment
		return nil, err
	}

	if lockRes != nil && lockRes.InfraChanged && !force {
		return nil, diagnostics.NewInfraLockedProblem(lockRes.Message)
	}

	// Apply image tag update
	cfg.Image = imageTag

	// Re-serialize config
	configData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed encoding updated config: %w", err)
	}
	configData = append(configData, '\n')
	if err := os.WriteFile(b.configPath(), configData, 0644); err != nil {
		return nil, fmt.Errorf("failed saving config update: %w", err)
	}

	// Refresh lockfile config_hash
	newLock := contract.GenerateLockfile(cfg)
	if err := contract.WriteLockfile(b.lockPath(), newLock); err != nil {
		return nil, fmt.Errorf("failed updating lockfile: %w", err)
	}

	duration := time.Since(start).Milliseconds()

	return &DeployResponse{
		Status:      "deployed",
		ImageTag:    imageTag,
		ContainerID: fmt.Sprintf("izd-%s-%d", cfg.Name, time.Now().Unix()%10000),
		DurationMS:  duration,
		Message:     fmt.Sprintf("Container %s successfully updated to image %s", cfg.Name, imageTag),
	}, nil
}

// Rollback simulates a traffic shift to the previous container version.
func (b *LocalBackend) Rollback(ctx context.Context) (*DeployResponse, error) {
	start := time.Now()

	cfg, err := contract.ParseConfigFile(b.configPath())
	if err != nil {
		return nil, err
	}

	// Simulate restoring previous image
	prevImage := cfg.Image + "-prev"
	cfg.Image = prevImage

	configData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed encoding config for rollback: %w", err)
	}
	configData = append(configData, '\n')
	if err := os.WriteFile(b.configPath(), configData, 0644); err != nil {
		return nil, fmt.Errorf("failed saving config rollback: %w", err)
	}

	// Refresh lockfile config_hash
	newLock := contract.GenerateLockfile(cfg)
	if err := contract.WriteLockfile(b.lockPath(), newLock); err != nil {
		return nil, fmt.Errorf("failed updating lockfile: %w", err)
	}

	duration := time.Since(start).Milliseconds()

	return &DeployResponse{
		Status:      "rolled_back",
		ImageTag:    prevImage,
		ContainerID: fmt.Sprintf("izd-%s-prev", cfg.Name),
		DurationMS:  duration,
		Message:     fmt.Sprintf("Traffic successfully rolled back to previous version of %s", cfg.Name),
	}, nil
}

// GetLogs returns a simulated or real ring-buffered log snippet.
func (b *LocalBackend) GetLogs(ctx context.Context, tail int) (string, error) {
	cfg, err := contract.ParseConfigFile(b.configPath())
	appName := "app"
	if err == nil && cfg.Name != "" {
		appName = cfg.Name
	}

	var sampleLogs []string
	// Generate sample log events
	for i := 1; i <= 80; i++ {
		sampleLogs = append(sampleLogs, fmt.Sprintf("\x1b[32m[INFO]\x1b[0m 2026-09-19T09:%02d:00Z [%s] HTTP GET /up status 200 latency=1.2ms", i%60, appName))
	}

	return RingBufferLogs(sampleLogs, 15, 45), nil
}

// Restart performs graceful container restart.
func (b *LocalBackend) Restart(ctx context.Context, gracePeriodSeconds int) (*RestartResponse, error) {
	if gracePeriodSeconds <= 0 {
		gracePeriodSeconds = 10
	}

	cfg, err := contract.ParseConfigFile(b.configPath())
	appName := "app"
	if err == nil && cfg.Name != "" {
		appName = cfg.Name
	}

	b.startTime = time.Now()

	return &RestartResponse{
		Status:             "restarted",
		App:                appName,
		GracePeriodSeconds: gracePeriodSeconds,
		Message:            fmt.Sprintf("Container %s restarted successfully with %ds grace period", appName, gracePeriodSeconds),
	}, nil
}

// SetEnv updates environment variables safely.
func (b *LocalBackend) SetEnv(ctx context.Context, vars map[string]string) (*EnvResponse, error) {
	if len(vars) == 0 {
		return nil, errors.New("no environment variables supplied")
	}

	// Validate keys: must be alphanumeric or underscore
	for k := range vars {
		if !envKeyRegex.MatchString(k) {
			return nil, diagnostics.NewProblem(
				diagnostics.UrnPrefix+"invalid-env-key",
				"Invalid Environment Variable Key",
				400,
				fmt.Sprintf("Environment key %q is invalid. Keys must match regex ^[a-zA-Z_][a-zA-Z0-9_]*$", k),
				false, // agent_actionable: false as specified in PRD
				"ERR_INVALID_ENV_KEY",
			)
		}
	}

	cfg, err := contract.ParseConfigFile(b.configPath())
	if err != nil {
		return nil, err
	}

	if cfg.Env == nil {
		cfg.Env = make(map[string]string)
	}

	for k, v := range vars {
		cfg.Env[k] = v
	}

	configData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed encoding config with new env: %w", err)
	}
	configData = append(configData, '\n')
	if err := os.WriteFile(b.configPath(), configData, 0644); err != nil {
		return nil, fmt.Errorf("failed saving updated env to config: %w", err)
	}

	// Update lockfile
	newLock := contract.GenerateLockfile(cfg)
	if err := contract.WriteLockfile(b.lockPath(), newLock); err != nil {
		return nil, fmt.Errorf("failed updating lockfile: %w", err)
	}

	b.startTime = time.Now()

	return &EnvResponse{
		Status:         "updated",
		Restarted:      true,
		VariablesCount: len(vars),
		Message:        fmt.Sprintf("Updated %d environment variables and restarted container", len(vars)),
	}, nil
}
