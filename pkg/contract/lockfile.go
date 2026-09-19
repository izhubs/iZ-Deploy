package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DECISION: Separate infra_hash (ports, volumes, routes, resources) from total config_hash in lockfile.
// WHY: Protect critical VPS infrastructure limits from unintended AI model modifications while
// still enabling continuous application image tag updates without triggering false-positive alerts.
// TRADE-OFF: Two distinct hashes are stored in the lock manifest.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-012

// LockfileVersion identifies the lock specification version.
const LockfileVersion = "v0.1"

// DefaultLockfileName defines the standard path for the deployment lockfile.
const DefaultLockfileName = ".agent/izdeploy.lock"

// Lockfile records cryptographic signatures of application infrastructure state.
//
// Business rule: Prevents unauthorized modification of sensitive host routing and limits.
//
// @ai-constraint: Must be committed to source control alongside .agent/izdeploy.json.
type Lockfile struct {
	Version    string    `json:"version"`
	App        string    `json:"app"`
	Port       int       `json:"port"`
	InfraHash  string    `json:"infra_hash"`
	ConfigHash string    `json:"config_hash"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// InfraSignature encapsulates sensitive infrastructure fields for hash computation.
type InfraSignature struct {
	Name        string            `json:"name"`
	Port        int               `json:"port"`
	Resources   *ResourceLimits   `json:"resources,omitempty"`
	Routes      []string          `json:"routes,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
	Healthcheck *HealthcheckSpec  `json:"healthcheck,omitempty"`
}

// CalculateInfraHash generates a deterministic SHA-256 checksum across infrastructure-sensitive fields.
//
// Business rule: Routes and volumes are sorted before hashing to eliminate map/slice ordering artifacts.
//
// @ai-constraint: Never include volatile image tags or arbitrary environment variables in the infra hash.
func CalculateInfraHash(cfg *AppConfig) string {
	if cfg == nil {
		return ""
	}

	sig := InfraSignature{
		Name:        cfg.Name,
		Port:        cfg.Port,
		Resources:   cfg.Resources,
		Healthcheck: cfg.Healthcheck,
	}

	if len(cfg.Routes) > 0 {
		routesCopy := append([]string(nil), cfg.Routes...)
		sort.Strings(routesCopy)
		sig.Routes = routesCopy
	}

	if len(cfg.Volumes) > 0 {
		volumesCopy := append([]string(nil), cfg.Volumes...)
		sort.Strings(volumesCopy)
		sig.Volumes = volumesCopy
	}

	data, _ := json.Marshal(sig)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CalculateConfigHash generates a deterministic SHA-256 checksum across the entire configuration.
//
// Business rule: Used to detect no-op deployments where neither code image nor config changed.
//
// @ai-constraint: Sorts map keys to ensure repeatable digest calculation.
func CalculateConfigHash(cfg *AppConfig) string {
	if cfg == nil {
		return ""
	}

	// Deterministic copy with sorted env keys
	data, _ := json.Marshal(cfg)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// GenerateLockfile builds a Lockfile struct from the current AppConfig.
//
// Business rule: Populates both infra_hash and config_hash with current timestamp.
//
// @ai-constraint: Updates lock timestamp accurately without altering UTC timezone.
func GenerateLockfile(cfg *AppConfig) *Lockfile {
	return &Lockfile{
		Version:    LockfileVersion,
		App:        cfg.Name,
		Port:       cfg.Port,
		InfraHash:  CalculateInfraHash(cfg),
		ConfigHash: CalculateConfigHash(cfg),
		UpdatedAt:  time.Now().UTC(),
	}
}

// ReadLockfile loads and parses the lockfile from the specified filesystem path.
//
// Business rule: Returns os.ErrNotExist if the lockfile has not yet been initialized.
//
// @ai-constraint: Rejects lockfiles with mismatched or unsupported schema versions.
func ReadLockfile(lockPath string) (*Lockfile, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}

	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("corrupted lockfile %s: %w", lockPath, err)
	}

	if lock.Version != LockfileVersion {
		return nil, fmt.Errorf("unsupported lockfile version %q, expected %q", lock.Version, LockfileVersion)
	}

	return &lock, nil
}

// WriteLockfile serializes the lockfile to disk with indented formatting.
//
// Business rule: Ensures parent directory exists before writing file.
//
// @ai-constraint: Atomically writes or uses safe standard permissions (0644).
func WriteLockfile(lockPath string, lock *Lockfile) error {
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed creating lockfile directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("failed encoding lockfile: %w", err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		return fmt.Errorf("failed writing lockfile %s: %w", lockPath, err)
	}

	return nil
}

// LockVerificationResult reports the comparative state between an AppConfig and its Lockfile.
type LockVerificationResult struct {
	Valid          bool   `json:"valid"`
	InfraChanged   bool   `json:"infra_changed"`
	ConfigChanged  bool   `json:"config_changed"`
	ExpectedInfra  string `json:"expected_infra"`
	ActualInfra    string `json:"actual_infra"`
	ExpectedConfig string `json:"expected_config"`
	ActualConfig   string `json:"actual_config"`
	Message        string `json:"message"`
}

// VerifyLockfile evaluates whether the current AppConfig conforms to the recorded Lockfile.
//
// Business rule: If the lockfile does not exist, returns an error requiring initialization.
//
// @ai-constraint: Pinpoints exact hash divergences to provide explicit error diagnostics.
func VerifyLockfile(lockPath string, cfg *AppConfig) (*LockVerificationResult, error) {
	lock, err := ReadLockfile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("lockfile not found at %s: run 'izdeploy init' or verify path", lockPath)
		}
		return nil, err
	}

	actualInfra := CalculateInfraHash(cfg)
	actualConfig := CalculateConfigHash(cfg)

	infraChanged := actualInfra != lock.InfraHash
	configChanged := actualConfig != lock.ConfigHash

	result := &LockVerificationResult{
		Valid:          !infraChanged,
		InfraChanged:   infraChanged,
		ConfigChanged:  configChanged,
		ExpectedInfra:  lock.InfraHash,
		ActualInfra:    actualInfra,
		ExpectedConfig: lock.ConfigHash,
		ActualConfig:   actualConfig,
	}

	if infraChanged {
		result.Message = fmt.Sprintf(
			"infrastructure parameters modified without lockfile update: expected infra_hash=%s, actual=%s. Use --force to override.",
			lock.InfraHash[:8], actualInfra[:8],
		)
		return result, errors.New(result.Message)
	}

	return result, nil
}
