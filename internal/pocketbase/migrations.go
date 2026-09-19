// Package pocketbase provides embedded SQLite data store management via PocketBase.
package pocketbase

import (
	"fmt"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/models/schema"
	"github.com/pocketbase/pocketbase/tools/types"
)

// DECISION: Enforce SQLite WAL mode and schema auto-migration on boot.
// WHY: WAL mode allows concurrent readers while single writer commits, preventing SQLITE_BUSY.
// TRADE-OFF: Retains wal and shm auxiliary files next to primary database file.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-004

// ConfigurePragmas applies SQLite performance and concurrency settings.
//
// Business rule: WAL mode and busy timeout must be configured before handling API traffic.
//
// @ai-constraint: Do not set synchronous to OFF; NORMAL preserves integrity across OS flushes.
func ConfigurePragmas(pbApp *pocketbase.PocketBase) error {
	queries := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
	}
	for _, query := range queries {
		if _, err := pbApp.Dao().DB().NewQuery(query).Execute(); err != nil {
			return fmt.Errorf("failed executing pragma %q: %w", query, err)
		}
	}
	return nil
}

// EnsureCollections creates system collections if they do not yet exist.
//
// Business rule: Migration is idempotent and runs during node boot before any deployment.
//
// @ai-constraint: Never delete existing collection schema fields; append-only schema changes.
func EnsureCollections(pbApp *pocketbase.PocketBase) error {
	if err := ensureAppsCollection(pbApp); err != nil {
		return err
	}
	if err := ensureDeploymentsCollection(pbApp); err != nil {
		return err
	}
	return ensureEnvVarsCollection(pbApp)
}

// ensureAppsCollection provisions the apps collection metadata and unique index.
func ensureAppsCollection(pbApp *pocketbase.PocketBase) error {
	_, err := pbApp.Dao().FindCollectionByNameOrId(CollectionApps)
	if err == nil {
		return nil
	}

	col := &models.Collection{
		Name: CollectionApps,
		Type: models.CollectionTypeBase,
		Schema: schema.NewSchema(
			&schema.SchemaField{Name: "name", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "port", Type: schema.FieldTypeNumber, Required: true},
			&schema.SchemaField{Name: "image", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "status", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "container_id", Type: schema.FieldTypeText},
		),
		Indexes: types.JsonArray[string]{
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_apps_name ON apps (name)",
		},
	}
	return pbApp.Dao().SaveCollection(col)
}

// ensureDeploymentsCollection provisions the deployments collection for rollout auditing.
func ensureDeploymentsCollection(pbApp *pocketbase.PocketBase) error {
	_, err := pbApp.Dao().FindCollectionByNameOrId(CollectionDeployments)
	if err == nil {
		return nil
	}

	col := &models.Collection{
		Name: CollectionDeployments,
		Type: models.CollectionTypeBase,
		Schema: schema.NewSchema(
			&schema.SchemaField{Name: "app_id", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "version", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "status", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "logs", Type: schema.FieldTypeText},
			&schema.SchemaField{Name: "deployed_at", Type: schema.FieldTypeText},
		),
		Indexes: types.JsonArray[string]{
			"CREATE INDEX IF NOT EXISTS idx_deployments_app_id ON deployments (app_id)",
		},
	}
	return pbApp.Dao().SaveCollection(col)
}

// ensureEnvVarsCollection provisions key-value storage for application runtime secrets.
func ensureEnvVarsCollection(pbApp *pocketbase.PocketBase) error {
	_, err := pbApp.Dao().FindCollectionByNameOrId(CollectionEnvVars)
	if err == nil {
		return nil
	}

	col := &models.Collection{
		Name: CollectionEnvVars,
		Type: models.CollectionTypeBase,
		Schema: schema.NewSchema(
			&schema.SchemaField{Name: "app_id", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "key", Type: schema.FieldTypeText, Required: true},
			&schema.SchemaField{Name: "value", Type: schema.FieldTypeText, Required: true},
		),
		Indexes: types.JsonArray[string]{
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_env_vars_app_key ON env_vars (app_id, key)",
		},
	}
	return pbApp.Dao().SaveCollection(col)
}
