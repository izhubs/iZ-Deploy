// Package main provides the host daemon and internal HTTP control API for izdeploy.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/izhubs/izdeploy/internal/pocketbase"
	"github.com/izhubs/izdeploy/pkg/docker"
)

// DeployRequest defines the inbound JSON body for container rollout.
type DeployRequest struct {
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

// RestartRequest defines the inbound payload for restarting an application container.
type RestartRequest struct {
	App string `json:"app"`
}

// EnvRequest defines the inbound payload for mutating environment key-value pairs.
type EnvRequest struct {
	App string            `json:"app"`
	Env map[string]string `json:"env"`
}

func (s *AgentServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	dockerStatus := "connected"
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.dockerClient.Ping(ctx); err != nil {
		dockerStatus = "unavailable"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"version":  Version,
		"database": "healthy",
		"docker":   dockerStatus,
	})
}

func (s *AgentServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	appName := r.URL.Query().Get("app")
	if appName == "" {
		appsList, err := s.storage.ListApps()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"apps": appsList})
		return
	}

	appRecord, err := s.storage.GetApp(appName)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not registered")
		return
	}

	responsePayload := map[string]any{"app": appRecord}
	if appRecord.ContainerID != "" {
		containerStatus, _ := s.dockerClient.InspectContainer(r.Context(), appRecord.ContainerID)
		responsePayload["container"] = containerStatus
		metrics, _ := s.dockerClient.ContainerStatsOneShot(r.Context(), appRecord.ContainerID)
		responsePayload["metrics"] = metrics
	}

	writeJSON(w, http.StatusOK, responsePayload)
}

func (s *AgentServer) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	if req.Name == "" || req.Image == "" || req.Port <= 0 {
		writeError(w, http.StatusBadRequest, "fields 'name', 'image', and 'port' are required")
		return
	}

	containerID, err := s.executeDeployment(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"app":          req.Name,
		"container_id": containerID,
	})
}

func (s *AgentServer) executeDeployment(ctx context.Context, req DeployRequest) (string, error) {
	appRecord, err := s.storage.GetApp(req.Name)
	if err != nil && errors.Is(err, pocketbase.ErrAppNotFound) {
		appRecord = &pocketbase.App{
			Name:   req.Name,
			Port:   req.Port,
			Image:  req.Image,
			Status: pocketbase.AppStatusPending,
		}
		_ = s.storage.SaveApp(appRecord)
		appRecord, _ = s.storage.GetApp(req.Name)
	}

	// Merge stored environment variables (TASK-008)
	mergedEnv := make(map[string]string)
	if appRecord != nil && appRecord.ID != "" {
		storedEnvs, _ := s.storage.GetEnvVars(appRecord.ID)
		for key, val := range storedEnvs {
			mergedEnv[key] = val
		}
	}
	for key, val := range req.Env {
		mergedEnv[key] = val
	}

	if err := s.dockerClient.PullImage(ctx, req.Image); err != nil {
		return "", err
	}

	spec := docker.DeploySpec{
		Name:          req.Name,
		Image:         req.Image,
		Port:          req.Port,
		HostIP:        req.HostIP,
		Env:           mergedEnv,
		Volumes:       req.Volumes,
		MemoryLimitMB: req.MemoryLimitMB,
		CPULimit:      req.CPULimit,
		Labels:        req.Labels,
	}

	containerID, err := s.dockerClient.DeployContainer(ctx, spec)
	if err != nil {
		return "", err
	}

	if appRecord != nil {
		appRecord.Status = pocketbase.AppStatusRunning
		appRecord.ContainerID = containerID
		appRecord.Image = req.Image
		appRecord.Port = req.Port
		_ = s.storage.SaveApp(appRecord)

		_ = s.storage.RecordDeployment(&pocketbase.Deployment{
			AppID:      appRecord.ID,
			Version:    req.Image,
			Status:     pocketbase.DeploymentStatusDeployed,
			Logs:       "Container deployed successfully",
			DeployedAt: time.Now().UTC(),
		})
	}

	go func() {
		pruneCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = s.dockerClient.PruneDanglingImages(pruneCtx)
	}()

	return containerID, nil
}

func (s *AgentServer) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RestartRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	appName := req.App
	if appName == "" {
		appName = r.URL.Query().Get("app")
	}
	if appName == "" {
		writeError(w, http.StatusBadRequest, "parameter 'app' is required")
		return
	}

	appRecord, err := s.storage.GetApp(appName)
	if err != nil || appRecord.ContainerID == "" {
		writeError(w, http.StatusNotFound, "application container not found")
		return
	}

	if err := s.dockerClient.RestartContainer(r.Context(), appRecord.ContainerID, docker.DefaultGracePeriodSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"app":     appName,
		"status":  "restarted",
	})
}

func (s *AgentServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	appName := r.URL.Query().Get("app")
	if appName == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'app' is required")
		return
	}

	appRecord, err := s.storage.GetApp(appName)
	if err != nil || appRecord.ContainerID == "" {
		writeError(w, http.StatusNotFound, "application container not found")
		return
	}

	tailCount, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tailCount <= 0 {
		tailCount = docker.DefaultMaxLinesCap
	}

	ringBuffer, err := s.dockerClient.GetContainerLogs(r.Context(), appRecord.ContainerID, tailCount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"app":         appName,
		"total_lines": ringBuffer.TotalCount(),
		"lines":       ringBuffer.Lines(),
		"raw":         ringBuffer.String(),
	})
}

func (s *AgentServer) handleEnv(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.handleGetEnv(w, r)
		return
	}
	if r.Method == http.MethodPost {
		s.handlePostEnv(w, r)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *AgentServer) handleGetEnv(w http.ResponseWriter, r *http.Request) {
	appName := r.URL.Query().Get("app")
	if appName == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'app' is required")
		return
	}

	appRecord, err := s.storage.GetApp(appName)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}

	envVars, err := s.storage.GetEnvVars(appRecord.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"app": appName,
		"env": envVars,
	})
}

func (s *AgentServer) handlePostEnv(w http.ResponseWriter, r *http.Request) {
	var req EnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	if req.App == "" {
		writeError(w, http.StatusBadRequest, "field 'app' is required")
		return
	}

	appRecord, err := s.storage.GetApp(req.App)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}

	for key, val := range req.Env {
		if err := s.storage.SetEnvVar(appRecord.ID, key, val); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"app":     req.App,
		"count":   len(req.Env),
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]any{
		"error":   message,
		"status":  statusCode,
	})
}
