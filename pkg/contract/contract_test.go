package contract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigBytes_Valid(t *testing.T) {
	raw := []byte(`{
		"name": "web-service",
		"port": 3000,
		"image": "ghcr.io/org/web:v1.0.0",
		"env": {
			"NODE_ENV": "production"
		},
		"healthcheck": {
			"path": "/healthz",
			"interval_seconds": 5,
			"timeout_seconds": 2,
			"max_retries": 3
		},
		"resources": {
			"memory_mb": 512,
			"cpu_cores": 0.5
		},
		"routes": ["app.example.com"],
		"volumes": ["/host/data:/data"]
	}`)

	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing valid config: %v", err)
	}

	if cfg.Name != "web-service" || cfg.Port != 3000 {
		t.Errorf("unexpected parsed values: name=%s, port=%d", cfg.Name, cfg.Port)
	}

	if err := Validate(cfg); err != nil {
		t.Errorf("validation failed on valid config: %v", err)
	}
}

func TestParseConfigBytes_BuildSpec(t *testing.T) {
	raw := []byte(`{
		"name": "build-app",
		"port": 8080,
		"image": "ghcr.io/org/build-app:latest",
		"build": {
			"mode": "github-actions",
			"builder": "nixpacks"
		}
	}`)

	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing config with build: %v", err)
	}

	if cfg.Build == nil {
		t.Fatal("expected cfg.Build to be non-nil")
	}
	if cfg.Build.Mode != "github-actions" || cfg.Build.Builder != "nixpacks" {
		t.Errorf("unexpected build values: mode=%s, builder=%s", cfg.Build.Mode, cfg.Build.Builder)
	}

	if err := Validate(cfg); err != nil {
		t.Errorf("validation failed on valid build config: %v", err)
	}
}

func TestParseConfigBytes_SyntaxError(t *testing.T) {
	raw := []byte("{\n  \"name\": \"app\",\n  \"port\": 3000,\n}")

	_, err := ParseConfigBytes(raw)
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}

	syntaxErr, ok := err.(*SyntaxError)
	if !ok {
		t.Logf("error is: %v", err)
	} else if syntaxErr.Line == 0 {
		t.Errorf("expected line number > 0, got %d", syntaxErr.Line)
	}
}

func TestParseConfigBytes_UnknownField(t *testing.T) {
	raw := []byte(`{
		"name": "app",
		"port": 3000,
		"image": "my-image:latest",
		"unknown_field": "disallowed"
	}`)

	_, err := ParseConfigBytes(raw)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestValidate_Constraints(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AppConfig
		wantErr bool
	}{
		{
			name: "Valid Minimal",
			cfg: AppConfig{
				Name:  "my-app",
				Port:  8080,
				Image: "ghcr.io/org/app:latest",
			},
			wantErr: false,
		},
		{
			name: "Invalid Name Uppercase",
			cfg: AppConfig{
				Name:  "My-App",
				Port:  8080,
				Image: "app:latest",
			},
			wantErr: true,
		},
		{
			name: "Invalid Port Zero",
			cfg: AppConfig{
				Name:  "app",
				Port:  0,
				Image: "app:latest",
			},
			wantErr: true,
		},
		{
			name: "Invalid Port Exceeds Max",
			cfg: AppConfig{
				Name:  "app",
				Port:  70000,
				Image: "app:latest",
			},
			wantErr: true,
		},
		{
			name: "Missing Image",
			cfg: AppConfig{
				Name:  "app",
				Port:  8080,
				Image: "",
			},
			wantErr: true,
		},
		{
			name: "Invalid Memory Limit",
			cfg: AppConfig{
				Name:  "app",
				Port:  8080,
				Image: "app:latest",
				Resources: &ResourceLimits{
					MemoryMB: 16, // Below minimum 32MB
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid Volume Format",
			cfg: AppConfig{
				Name:    "app",
				Port:    8080,
				Image:   "app:latest",
				Volumes: []string{"bad-volume-no-colon"},
			},
			wantErr: true,
		},
		{
			name: "Valid Build Spec",
			cfg: AppConfig{
				Name:  "app",
				Port:  8080,
				Image: "app:latest",
				Build: &BuildSpec{
					Mode:    "github-actions",
					Builder: "nixpacks",
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid Build Mode",
			cfg: AppConfig{
				Name:  "app",
				Port:  8080,
				Image: "app:latest",
				Build: &BuildSpec{
					Mode:    "invalid-mode",
					Builder: "nixpacks",
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid Build Builder",
			cfg: AppConfig{
				Name:  "app",
				Port:  8080,
				Image: "app:latest",
				Build: &BuildSpec{
					Mode:    "host",
					Builder: "invalid-builder",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLockfile_GenerationAndVerification(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, ".agent", "izdeploy.lock")

	cfg := &AppConfig{
		Name:  "service-alpha",
		Port:  3000,
		Image: "ghcr.io/org/alpha:v1.0.0",
		Resources: &ResourceLimits{
			MemoryMB: 256,
			CPUCores: 1.0,
		},
	}

	lock := GenerateLockfile(cfg)
	if lock.InfraHash == "" || lock.ConfigHash == "" {
		t.Fatal("hashes must not be empty")
	}

	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("failed writing lockfile: %v", err)
	}

	// Verification with matching config
	res, err := VerifyLockfile(lockPath, cfg)
	if err != nil {
		t.Fatalf("expected lockfile verification to succeed, got error: %v", err)
	}
	if !res.Valid || res.InfraChanged {
		t.Errorf("expected valid verification: valid=%v, infraChanged=%v", res.Valid, res.InfraChanged)
	}

	// Verification with modified image (infra unchanged)
	cfgImageUpdated := *cfg
	cfgImageUpdated.Image = "ghcr.io/org/alpha:v1.0.1"
	res2, err := VerifyLockfile(lockPath, &cfgImageUpdated)
	if err != nil {
		t.Fatalf("expected infra verification to pass on image change, got: %v", err)
	}
	if !res2.Valid || res2.InfraChanged || !res2.ConfigChanged {
		t.Errorf("expected infra unchanged and config changed: valid=%v, infraChanged=%v, configChanged=%v",
			res2.Valid, res2.InfraChanged, res2.ConfigChanged)
	}

	// Verification with modified port (infra changed) -> must fail
	cfgPortChanged := *cfg
	cfgPortChanged.Port = 4000
	_, err = VerifyLockfile(lockPath, &cfgPortChanged)
	if err == nil {
		t.Fatal("expected lockfile verification failure when port modified, got nil")
	}
}

func TestBridgeFiles_Generation(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &AppConfig{
		Name: "test-app",
		Port: 8080,
	}

	if err := GenerateBridgeFiles(tempDir, cfg); err != nil {
		t.Fatalf("failed generating bridge files: %v", err)
	}

	cursorPath := filepath.Join(tempDir, CursorRulePath)
	claudePath := filepath.Join(tempDir, ClaudeGuidePath)

	if _, err := os.Stat(cursorPath); err != nil {
		t.Errorf("expected cursor rule file at %s: %v", cursorPath, err)
	}
	if _, err := os.Stat(claudePath); err != nil {
		t.Errorf("expected claude guide file at %s: %v", claudePath, err)
	}
}
