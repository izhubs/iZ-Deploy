// Package contract defines the declarative specification, parser, validator,
// and lockfile signing mechanisms for izDeploy application definitions (.agent/izdeploy.json).
package contract

// DECISION: Embed declarative configuration schema in typed Go structs with optional pointers.
// WHY: Optional pointers differentiate between omitted fields and zero-valued primitives,
// enabling accurate schema validation and deterministic default value injection.
// TRADE-OFF: Requires nil checks when accessing Healthcheck and Resources sub-structs.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-006

// Default configuration constants conforming to izDeploy v0.1 spec.
const (
	DefaultHealthcheckPath     = "/up"
	DefaultHealthcheckInterval = 3
	DefaultHealthcheckTimeout  = 5
	DefaultHealthcheckRetries  = 10
	DefaultResourceMemoryMB    = 256
	DefaultResourceCPUCores    = 1.0
	DefaultBuildMode           = "github-actions"
	DefaultBuildBuilder        = "nixpacks"
	MinPort                    = 1
	MaxPort                    = 65535
	MinMemoryMB                = 32
	MinCPUCores                = 0.1
)

// BuildSpec defines application build strategy and tooling.
//
// Business rule: Offloads builds to external CI/CD or executes local/host builds.
//
// @ai-constraint: Mode defaults to "github-actions", builder defaults to "nixpacks".
type BuildSpec struct {
	Mode    string `json:"mode,omitempty"`    // "github-actions" (default), "host", "local", "cloud"
	Builder string `json:"builder,omitempty"` // "nixpacks" (default), "dockerfile"
}

// AppConfig represents the root application contract schema (.agent/izdeploy.json).
//
// Business rule: name, port, and image are mandatory for container execution.
//
// @ai-constraint: Sensitive infrastructure fields (port, routes, volumes, resources)
// are cryptographically locked in .agent/izdeploy.lock to prevent unauthorized mutation.
type AppConfig struct {
	Name        string            `json:"name"`
	Port        int               `json:"port"`
	Image       string            `json:"image"`
	Env         map[string]string `json:"env,omitempty"`
	Build       *BuildSpec        `json:"build,omitempty"`
	Healthcheck *HealthcheckSpec  `json:"healthcheck,omitempty"`
	Resources   *ResourceLimits   `json:"resources,omitempty"`
	Routes      []string          `json:"routes,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
}

// HealthcheckSpec defines the active liveness probe configuration for zero-downtime swaps.
//
// Business rule: Reverse proxy kamal-proxy verifies this endpoint before shifting traffic.
//
// @ai-constraint: Probe interval must not exceed timeout to guarantee predictable recovery.
type HealthcheckSpec struct {
	Path            string `json:"path,omitempty"`
	IntervalSeconds int    `json:"interval_seconds,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
	MaxRetries      int    `json:"max_retries,omitempty"`
}

// ResourceLimits specifies cgroup v2 boundaries applied to the container.
//
// Business rule: Enforces host preservation on constrained VPS environments (>=512MB RAM).
//
// @ai-constraint: Memory allocations below 32MB are rejected as unsafe for standard runtimes.
type ResourceLimits struct {
	MemoryMB int     `json:"memory_mb,omitempty"`
	CPUCores float64 `json:"cpu_cores,omitempty"`
}

// ApplyDefaults injects standard baseline values for omitted optional configuration fields.
//
// Business rule: Ensures deterministic container behavior even when users author minimal configs.
//
// @ai-constraint: Never overwrite user-specified non-zero values during default injection.
func (c *AppConfig) ApplyDefaults() {
	if c.Healthcheck == nil {
		c.Healthcheck = &HealthcheckSpec{
			Path:            DefaultHealthcheckPath,
			IntervalSeconds: DefaultHealthcheckInterval,
			TimeoutSeconds:  DefaultHealthcheckTimeout,
			MaxRetries:      DefaultHealthcheckRetries,
		}
	} else {
		if c.Healthcheck.Path == "" {
			c.Healthcheck.Path = DefaultHealthcheckPath
		}
		if c.Healthcheck.IntervalSeconds <= 0 {
			c.Healthcheck.IntervalSeconds = DefaultHealthcheckInterval
		}
		if c.Healthcheck.TimeoutSeconds <= 0 {
			c.Healthcheck.TimeoutSeconds = DefaultHealthcheckTimeout
		}
		if c.Healthcheck.MaxRetries <= 0 {
			c.Healthcheck.MaxRetries = DefaultHealthcheckRetries
		}
	}

	if c.Resources == nil {
		c.Resources = &ResourceLimits{
			MemoryMB: DefaultResourceMemoryMB,
			CPUCores: DefaultResourceCPUCores,
		}
	} else {
		if c.Resources.MemoryMB <= 0 {
			c.Resources.MemoryMB = DefaultResourceMemoryMB
		}
		if c.Resources.CPUCores <= 0 {
			c.Resources.CPUCores = DefaultResourceCPUCores
		}
	}

	if c.Build != nil {
		if c.Build.Mode == "" {
			c.Build.Mode = DefaultBuildMode
		}
		if c.Build.Builder == "" {
			c.Build.Builder = DefaultBuildBuilder
		}
	}

	if c.Env == nil {
		c.Env = make(map[string]string)
	}
}
