// Package pocketbase_test verifies embedded database operations and collection migrations.
package pocketbase_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/izhubs/izdeploy/internal/pocketbase"
)

// TestPocketBaseEngine verifies bootstrap, migrations, and CRUD behaviors.
//
// Business rule: Database must operate fully functional on a clean directory with auto-migrations.
func TestPocketBaseEngine(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "pb_data")

	engine, err := pocketbase.NewEngine(dataDir)
	if err != nil {
		t.Fatalf("failed to initialize pocketbase engine: %v", err)
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil {
			t.Errorf("failed to close engine: %v", closeErr)
		}
	}()

	// 1. Test App persistence
	testApp := &pocketbase.App{
		Name:        "web-sample",
		Port:        3000,
		Image:       "ghcr.io/izhubs/sample:v1",
		Status:      pocketbase.AppStatusPending,
		ContainerID: "cid-12345",
	}

	if err := engine.SaveApp(testApp); err != nil {
		t.Fatalf("failed saving app: %v", err)
	}

	fetched, err := engine.GetApp("web-sample")
	if err != nil {
		t.Fatalf("failed retrieving app: %v", err)
	}
	if fetched.Name != "web-sample" || fetched.Port != 3000 {
		t.Errorf("unexpected app content: %+v", fetched)
	}

	// 2. Test Deployment recording
	dep := &pocketbase.Deployment{
		AppID:      fetched.ID,
		Version:    "v1.0.0",
		Status:     pocketbase.DeploymentStatusDeployed,
		Logs:       "Build finished successfully",
		DeployedAt: time.Now().UTC(),
	}
	if err := engine.RecordDeployment(dep); err != nil {
		t.Fatalf("failed recording deployment: %v", err)
	}

	deployments, err := engine.ListDeployments(fetched.ID, 10)
	if err != nil {
		t.Fatalf("failed listing deployments: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deployments))
	}
	if deployments[0].Version != "v1.0.0" {
		t.Errorf("expected version v1.0.0, got %s", deployments[0].Version)
	}

	// 3. Test EnvVars management
	if err := engine.SetEnvVar(fetched.ID, "PORT", "3000"); err != nil {
		t.Fatalf("failed setting env var: %v", err)
	}
	if err := engine.SetEnvVar(fetched.ID, "NODE_ENV", "production"); err != nil {
		t.Fatalf("failed setting env var: %v", err)
	}

	envVars, err := engine.GetEnvVars(fetched.ID)
	if err != nil {
		t.Fatalf("failed getting env vars: %v", err)
	}
	if len(envVars) != 2 || envVars["PORT"] != "3000" || envVars["NODE_ENV"] != "production" {
		t.Errorf("unexpected env vars: %+v", envVars)
	}

	// Delete one env var
	if err := engine.DeleteEnvVar(fetched.ID, "PORT"); err != nil {
		t.Fatalf("failed deleting env var: %v", err)
	}
	envVarsAfter, err := engine.GetEnvVars(fetched.ID)
	if err != nil {
		t.Fatalf("failed getting env vars after deletion: %v", err)
	}
	if len(envVarsAfter) != 1 || envVarsAfter["NODE_ENV"] != "production" {
		t.Errorf("unexpected env vars after deletion: %+v", envVarsAfter)
	}
}
