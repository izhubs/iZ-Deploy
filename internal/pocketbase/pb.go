// Package pocketbase provides embedded SQLite data store management via PocketBase.
package pocketbase

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/tools/migrate"
)

// ErrAppNotFound indicates that the requested application is not registered.
var ErrAppNotFound = errors.New("application record not found")

// Engine encapsulates the embedded PocketBase instance and persistence operations.
//
// Business rule: Engine runs embedded in-process to serve daemon queries with zero network overhead.
type Engine struct {
	pb      *pocketbase.PocketBase
	dataDir string
	mutex   sync.RWMutex
}

// NewEngine initializes and bootstraps the embedded database.
//
// Business rule: Data directory must be initialized and schema verified before returning.
//
// @ai-constraint: Always invoke Bootstrap() and ConfigurePragmas() before serving queries.
func NewEngine(dataDir string) (*Engine, error) {
	pbApp := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dataDir,
		HideStartBanner: true,
	})

	pbApp.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		e.App.Settings().Meta.AppName = "izDeploy Cloud"
		return nil
	})

	if err := pbApp.Bootstrap(); err != nil {
		return nil, fmt.Errorf("failed to bootstrap pocketbase: %w", err)
	}

	if err := ConfigurePragmas(pbApp); err != nil {
		_ = pbApp.ResetBootstrapState()
		return nil, fmt.Errorf("failed to configure pragmas: %w", err)
	}

	runner, err := migrate.NewRunner(pbApp.DB(), migrations.AppMigrations)
	if err != nil {
		_ = pbApp.ResetBootstrapState()
		return nil, fmt.Errorf("failed to create migration runner: %w", err)
	}
	if _, err := runner.Up(); err != nil {
		_ = pbApp.ResetBootstrapState()
		return nil, fmt.Errorf("failed running system migrations: %w", err)
	}

	if err := EnsureCollections(pbApp); err != nil {
		_ = pbApp.ResetBootstrapState()
		return nil, fmt.Errorf("failed to ensure collections: %w", err)
	}

	return &Engine{
		pb:      pbApp,
		dataDir: dataDir,
	}, nil
}

// GetApp retrieves an application metadata record by unique name.
//
// Business rule: App lookup must return ErrAppNotFound if the record is missing.
func (e *Engine) GetApp(name string) (*App, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	record, err := e.pb.Dao().FindFirstRecordByData(CollectionApps, "name", name)
	if err != nil {
		return nil, ErrAppNotFound
	}
	return recordToApp(record), nil
}

// SaveApp creates or updates an application metadata record.
//
// Business rule: Unique constraint on name prevents multiple records for identical app names.
func (e *Engine) SaveApp(appRecord *App) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	col, err := e.pb.Dao().FindCollectionByNameOrId(CollectionApps)
	if err != nil {
		return fmt.Errorf("apps collection missing: %w", err)
	}

	var record *models.Record
	if appRecord.ID != "" {
		record, _ = e.pb.Dao().FindRecordById(CollectionApps, appRecord.ID)
	}
	if record == nil {
		record, _ = e.pb.Dao().FindFirstRecordByData(CollectionApps, "name", appRecord.Name)
	}
	if record == nil {
		record = models.NewRecord(col)
	}

	record.Set("name", appRecord.Name)
	record.Set("port", appRecord.Port)
	record.Set("image", appRecord.Image)
	record.Set("status", appRecord.Status)
	record.Set("container_id", appRecord.ContainerID)

	if err := e.pb.Dao().SaveRecord(record); err != nil {
		return fmt.Errorf("failed to persist app record: %w", err)
	}
	appRecord.ID = record.Id
	return nil
}

// ListApps returns all registered applications.
func (e *Engine) ListApps() ([]*App, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	records, err := e.pb.Dao().FindRecordsByFilter(CollectionApps, "1=1", "name", 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed listing apps: %w", err)
	}

	appsList := make([]*App, 0, len(records))
	for _, record := range records {
		appsList = append(appsList, recordToApp(record))
	}
	return appsList, nil
}

// RecordDeployment appends a new rollout record for audit and status tracking.
func (e *Engine) RecordDeployment(depRecord *Deployment) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	col, err := e.pb.Dao().FindCollectionByNameOrId(CollectionDeployments)
	if err != nil {
		return fmt.Errorf("deployments collection missing: %w", err)
	}

	record := models.NewRecord(col)
	record.Set("app_id", depRecord.AppID)
	record.Set("version", depRecord.Version)
	record.Set("status", depRecord.Status)
	record.Set("logs", depRecord.Logs)
	if depRecord.DeployedAt.IsZero() {
		depRecord.DeployedAt = time.Now().UTC()
	}
	record.Set("deployed_at", depRecord.DeployedAt.Format(time.RFC3339))

	if err := e.pb.Dao().SaveRecord(record); err != nil {
		return fmt.Errorf("failed recording deployment: %w", err)
	}
	depRecord.ID = record.Id
	return nil
}

// ListDeployments returns the deployment history for a specific application.
func (e *Engine) ListDeployments(appID string, limitCount int) ([]*Deployment, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	records, err := e.pb.Dao().FindRecordsByFilter(
		CollectionDeployments,
		"app_id = {:appID}",
		"-created",
		limitCount,
		0,
		dbx.Params{"appID": appID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed listing deployments: %w", err)
	}

	deployments := make([]*Deployment, 0, len(records))
	for _, record := range records {
		deployments = append(deployments, recordToDeployment(record))
	}
	return deployments, nil
}

// GetEnvVars retrieves all environment variable pairs for an application.
func (e *Engine) GetEnvVars(appID string) (map[string]string, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	records, err := e.pb.Dao().FindRecordsByFilter(
		CollectionEnvVars,
		"app_id = {:appID}",
		"key",
		0,
		0,
		dbx.Params{"appID": appID},
	)
	if err != nil {
		return nil, fmt.Errorf("failed querying env vars: %w", err)
	}

	envMap := make(map[string]string, len(records))
	for _, record := range records {
		envMap[record.GetString("key")] = record.GetString("value")
	}
	return envMap, nil
}

// SetEnvVar inserts or updates an environment variable for an application.
func (e *Engine) SetEnvVar(appID, key, value string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	col, err := e.pb.Dao().FindCollectionByNameOrId(CollectionEnvVars)
	if err != nil {
		return fmt.Errorf("env_vars collection missing: %w", err)
	}

	records, _ := e.pb.Dao().FindRecordsByFilter(
		CollectionEnvVars,
		"app_id = {:appID} && key = {:key}",
		"",
		1,
		0,
		dbx.Params{"appID": appID, "key": key},
	)

	var record *models.Record
	if len(records) > 0 {
		record = records[0]
	} else {
		record = models.NewRecord(col)
	}

	record.Set("app_id", appID)
	record.Set("key", key)
	record.Set("value", value)

	return e.pb.Dao().SaveRecord(record)
}

// DeleteEnvVar removes an environment variable binding.
func (e *Engine) DeleteEnvVar(appID, key string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	records, err := e.pb.Dao().FindRecordsByFilter(
		CollectionEnvVars,
		"app_id = {:appID} && key = {:key}",
		"",
		1,
		0,
		dbx.Params{"appID": appID, "key": key},
	)
	if err != nil || len(records) == 0 {
		return nil
	}

	return e.pb.Dao().DeleteRecord(records[0])
}

// Close gracefully releases SQLite connection pools and background workers.
//
// @ai-constraint: Always call Close during daemon shutdown to flush WAL logs.
func (e *Engine) Close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.pb.ResetBootstrapState()
}

func recordToApp(record *models.Record) *App {
	return &App{
		ID:          record.Id,
		Name:        record.GetString("name"),
		Port:        record.GetInt("port"),
		Image:       record.GetString("image"),
		Status:      record.GetString("status"),
		ContainerID: record.GetString("container_id"),
		CreatedAt:   record.Created.Time(),
		UpdatedAt:   record.Updated.Time(),
	}
}

func recordToDeployment(record *models.Record) *Deployment {
	t, _ := time.Parse(time.RFC3339, record.GetString("deployed_at"))
	return &Deployment{
		ID:         record.Id,
		AppID:      record.GetString("app_id"),
		Version:    record.GetString("version"),
		Status:     record.GetString("status"),
		Logs:       record.GetString("logs"),
		DeployedAt: t,
	}
}
