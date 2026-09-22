package contract

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DECISION: Perform direct semantic validation in Go code rather than external schema validators.
// WHY: Eliminates heavy external CGO/JSON-schema dependencies while delivering custom RFC-style error messages.
// TRADE-OFF: Schema alterations require synchronized updates between Go validator and schema.json.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-006

var (
	// dnsSubdomainRegex matches RFC 1123 compliant DNS labels (lowercase alphanumeric with hyphens).
	dnsSubdomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// ValidationError details a specific field failure within the contract specification.
type ValidationError struct {
	Field   string `json:"field"`
	Reason  string `json:"reason"`
}

// Error returns a formatted single-line validation error representation.
func (v ValidationError) Error() string {
	return fmt.Sprintf("field %q: %s", v.Field, v.Reason)
}

// ValidationErrors aggregates multiple schema validation violations.
type ValidationErrors []ValidationError

// Error concatenates all individual validation errors with newline separation.
func (ve ValidationErrors) Error() string {
	var b strings.Builder
	for i, err := range ve {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(err.Error())
	}
	return b.String()
}

// Validate verifies structural constraints and semantic integrity of an AppConfig instance.
//
// Business rule: Enforces RFC 1123 naming, standard TCP port ranges, and valid container specs.
//
// @ai-constraint: Accumulates all validation violations before returning to minimize round-trip debug cycles.
func Validate(cfg *AppConfig) error {
	if cfg == nil {
		return errors.New("configuration object is nil")
	}

	var errs ValidationErrors

	// Validate Name
	if cfg.Name == "" {
		errs = append(errs, ValidationError{Field: "name", Reason: "application name is required"})
	} else if len(cfg.Name) > 63 {
		errs = append(errs, ValidationError{Field: "name", Reason: "application name must not exceed 63 characters"})
	} else if !dnsSubdomainRegex.MatchString(cfg.Name) {
		errs = append(errs, ValidationError{Field: "name", Reason: "application name must be DNS-safe (lowercase alphanumeric and hyphens, starting and ending with alphanumeric)"})
	}

	// Validate Port
	if cfg.Port < MinPort || cfg.Port > MaxPort {
		errs = append(errs, ValidationError{Field: "port", Reason: fmt.Sprintf("port must be an integer between %d and %d", MinPort, MaxPort)})
	}

	// Validate Strategy if supplied
	if cfg.Strategy != "" && cfg.Strategy != StrategyZeroDowntime && cfg.Strategy != StrategyRecreate {
		errs = append(errs, ValidationError{
			Field:  "strategy",
			Reason: fmt.Sprintf("strategy must be one of: %s, %s", StrategyZeroDowntime, StrategyRecreate),
		})
	}

	// Validate Image
	if strings.TrimSpace(cfg.Image) == "" {
		errs = append(errs, ValidationError{Field: "image", Reason: "container image reference is required"})
	}

	// Validate Healthcheck if supplied
	if cfg.Healthcheck != nil {
		if cfg.Healthcheck.Path != "" && !strings.HasPrefix(cfg.Healthcheck.Path, "/") {
			errs = append(errs, ValidationError{Field: "healthcheck.path", Reason: "healthcheck path must start with '/'"})
		}
		if cfg.Healthcheck.IntervalSeconds < 0 {
			errs = append(errs, ValidationError{Field: "healthcheck.interval_seconds", Reason: "interval_seconds must be a positive integer"})
		}
		if cfg.Healthcheck.TimeoutSeconds < 0 {
			errs = append(errs, ValidationError{Field: "healthcheck.timeout_seconds", Reason: "timeout_seconds must be a positive integer"})
		}
		if cfg.Healthcheck.MaxRetries < 0 {
			errs = append(errs, ValidationError{Field: "healthcheck.max_retries", Reason: "max_retries must be a positive integer"})
		}
	}

	// Validate Resources if supplied
	if cfg.Resources != nil {
		if cfg.Resources.MemoryMB > 0 && cfg.Resources.MemoryMB < MinMemoryMB {
			errs = append(errs, ValidationError{Field: "resources.memory_mb", Reason: fmt.Sprintf("memory_mb must be at least %dMB", MinMemoryMB)})
		}
		if cfg.Resources.CPUCores > 0 && cfg.Resources.CPUCores < MinCPUCores {
			errs = append(errs, ValidationError{Field: "resources.cpu_cores", Reason: fmt.Sprintf("cpu_cores must be at least %.1f", MinCPUCores)})
		}
	}

	// Validate Volumes if supplied
	for i, v := range cfg.Volumes {
		parts := strings.Split(v, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !strings.HasPrefix(parts[1], "/") {
			errs = append(errs, ValidationError{
				Field:  fmt.Sprintf("volumes[%d]", i),
				Reason: "volume must follow 'host_path:container_path' format with container_path starting with '/'",
			})
		}
	}

	// Validate Routes if supplied
	for i, r := range cfg.Routes {
		if strings.TrimSpace(r) == "" {
			errs = append(errs, ValidationError{
				Field:  fmt.Sprintf("routes[%d]", i),
				Reason: "route specifier must not be empty",
			})
		}
	}

	// Validate Build if supplied
	if cfg.Build != nil {
		if cfg.Build.Mode != "" {
			switch cfg.Build.Mode {
			case "github-actions", "host", "local", "cloud":
			default:
				errs = append(errs, ValidationError{
					Field:  "build.mode",
					Reason: "build mode must be one of: github-actions, host, local, cloud",
				})
			}
		}
		if cfg.Build.Builder != "" {
			switch cfg.Build.Builder {
			case "nixpacks", "dockerfile":
			default:
				errs = append(errs, ValidationError{
					Field:  "build.builder",
					Reason: "build builder must be one of: nixpacks, dockerfile",
				})
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}
