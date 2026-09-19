// Package main verifies the HTTP server routing and handlers.
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/izhubs/izdeploy/internal/pocketbase"
	"github.com/izhubs/izdeploy/pkg/docker"
)

func setupTestServer(t *testing.T) (*AgentServer, *pocketbase.Engine) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "pb_data")

	engine, err := pocketbase.NewEngine(dataDir)
	if err != nil {
		t.Fatalf("failed initializing test engine: %v", err)
	}

	// Docker client can be nil or initialized with empty host for routing tests
	dockerCli, _ := docker.NewClient("")
	server := NewAgentServer("127.0.0.1:0", engine, dockerCli)
	return server, engine
}

func TestServerHealth(t *testing.T) {
	server, engine := setupTestServer(t)
	defer func() { _ = engine.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed decoding json: %v", err)
	}
	if resp["status"] != "ok" || resp["version"] != Version {
		t.Errorf("unexpected health response: %+v", resp)
	}
}

func TestServerStatus_EmptyApps(t *testing.T) {
	server, engine := setupTestServer(t)
	defer func() { _ = engine.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	server.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed decoding json: %v", err)
	}
	if _, ok := resp["apps"]; !ok {
		t.Errorf("expected 'apps' field in response")
	}
}

func TestServerEnvEndpoints(t *testing.T) {
	server, engine := setupTestServer(t)
	defer func() { _ = engine.Close() }()

	// First register an app in DB
	testApp := &pocketbase.App{
		Name:   "worker-app",
		Port:   4000,
		Image:  "ghcr.io/org/worker:v1",
		Status: pocketbase.AppStatusPending,
	}
	if err := engine.SaveApp(testApp); err != nil {
		t.Fatalf("failed saving app: %v", err)
	}

	// POST /env
	envPayload := EnvRequest{
		App: "worker-app",
		Env: map[string]string{
			"LOG_LEVEL": "debug",
			"CONCURRENCY": "4",
		},
	}
	payloadBytes, _ := json.Marshal(envPayload)
	postReq := httptest.NewRequest(http.MethodPost, "/env", bytes.NewReader(payloadBytes))
	postRec := httptest.NewRecorder()

	server.httpServer.Handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from POST /env, got %d", postRec.Code)
	}

	// GET /env?app=worker-app
	getReq := httptest.NewRequest(http.MethodGet, "/env?app=worker-app", nil)
	getRec := httptest.NewRecorder()

	server.httpServer.Handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from GET /env, got %d", getRec.Code)
	}

	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("failed decoding json: %v", err)
	}
	envMap, ok := getResp["env"].(map[string]any)
	if !ok || envMap["LOG_LEVEL"] != "debug" || envMap["CONCURRENCY"] != "4" {
		t.Errorf("unexpected env response: %+v", getResp)
	}
}

func TestServerDeployValidation(t *testing.T) {
	server, engine := setupTestServer(t)
	defer func() { _ = engine.Close() }()

	// Empty payload should fail with 400 Bad Request
	emptyPayload := DeployRequest{
		Name: "",
	}
	payloadBytes, _ := json.Marshal(emptyPayload)
	req := httptest.NewRequest(http.MethodPost, "/deploy", bytes.NewReader(payloadBytes))
	rec := httptest.NewRecorder()

	server.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on invalid deploy payload, got %d", rec.Code)
	}
}
