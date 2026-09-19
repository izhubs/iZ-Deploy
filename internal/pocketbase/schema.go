// Package pocketbase provides embedded SQLite data store management via PocketBase.
package pocketbase

import "time"

// Collection name constants for schema isolation.
const (
	CollectionApps        = "apps"
	CollectionDeployments = "deployments"
	CollectionEnvVars     = "env_vars"
)

// App lifecycle status constants.
const (
	AppStatusPending = "pending"
	AppStatusRunning = "running"
	AppStatusStopped = "stopped"
	AppStatusFailed  = "failed"
)

// Deployment lifecycle status constants.
const (
	DeploymentStatusBuilding   = "building"
	DeploymentStatusDeployed   = "deployed"
	DeploymentStatusFailed     = "failed"
	DeploymentStatusRolledBack = "rolled_back"
)

// App represents an application definition registered on the local node.
//
// Business rule: The Name field must be unique per host and match DNS label constraints.
type App struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Port        int       `json:"port"`
	Image       string    `json:"image"`
	Status      string    `json:"status"`
	ContainerID string    `json:"container_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Deployment records an execution event of building or swapping a container.
//
// Business rule: Deployments are append-only audit entries tracking rollout history.
type Deployment struct {
	ID         string    `json:"id"`
	AppID      string    `json:"app_id"`
	Version    string    `json:"version"`
	Status     string    `json:"status"`
	Logs       string    `json:"logs"`
	DeployedAt time.Time `json:"deployed_at"`
}

// EnvVar represents a single environment key-value binding scoped to an application.
//
// Business rule: Keys are case-sensitive and must conform to POSIX environment variable naming.
type EnvVar struct {
	ID    string `json:"id"`
	AppID string `json:"app_id"`
	Key   string `json:"key"`
	Value string `json:"value"`
}
