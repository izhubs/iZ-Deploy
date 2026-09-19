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

func TestServerWebhook_Authentication(t *testing.T) {
	server, engine := setupTestServer(t)
	defer func() { _ = engine.Close() }()

	t.Setenv("IZDEPLOY_WEBHOOK_SECRET", "super-secret-token")

	payload := WebhookPayload{
		App:   "my-app",
		Image: "ghcr.io/org/my-app:v1.0.0",
		Port:  3000,
		Mode:  "pull",
	}
	payloadBytes, _ := json.Marshal(payload)

	// Case 1: Missing Token -> 401 Unauthorized with RFC 7807
	reqNoToken := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payloadBytes))
	recNoToken := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recNoToken, reqNoToken)

	if recNoToken.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on missing token, got %d", recNoToken.Code)
	}
	if ct := recNoToken.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("expected application/problem+json, got %s", ct)
	}
	var probResp map[string]any
	if err := json.NewDecoder(recNoToken.Body).Decode(&probResp); err != nil {
		t.Fatalf("failed decoding problem details: %v", err)
	}
	if probResp["error_code"] != "ERR_UNAUTHORIZED" || probResp["status"] != float64(401) {
		t.Errorf("unexpected problem details body: %+v", probResp)
	}

	// Case 2: Wrong Token -> 401 Unauthorized
	reqWrong := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payloadBytes))
	reqWrong.Header.Set("Authorization", "Bearer invalid-token")
	recWrong := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recWrong, reqWrong)

	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on wrong token, got %d", recWrong.Code)
	}

	// Case 3: Valid Token via Authorization: Bearer <secret> on /webhook
	reqBearer := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payloadBytes))
	reqBearer.Header.Set("Authorization", "Bearer super-secret-token")
	recBearer := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recBearer, reqBearer)

	// Since docker is mock/nil, execution reaches deployment phase (non-401)
	if recBearer.Code == http.StatusUnauthorized {
		t.Errorf("expected authentication success with valid Bearer token, got 401")
	}

	// Case 4: Valid Token via X-Izdeploy-Token on /webhook/deploy
	reqHeader := httptest.NewRequest(http.MethodPost, "/webhook/deploy", bytes.NewReader(payloadBytes))
	reqHeader.Header.Set("X-Izdeploy-Token", "super-secret-token")
	recHeader := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recHeader, reqHeader)

	if recHeader.Code == http.StatusUnauthorized {
		t.Errorf("expected authentication success with X-Izdeploy-Token, got 401")
	}
}

func TestServerWebhook_Validation(t *testing.T) {
	server, engine := setupTestServer(t)
	defer func() { _ = engine.Close() }()

	// Without secret set, requests are allowed through auth gate
	t.Setenv("IZDEPLOY_WEBHOOK_SECRET", "")

	// Invalid JSON payload -> 400 Bad Request
	reqInvalidJSON := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte("{invalid-json")))
	recInvalidJSON := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recInvalidJSON, reqInvalidJSON)
	if recInvalidJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid JSON, got %d", recInvalidJSON.Code)
	}

	// Missing required fields -> 400 Bad Request
	missingFieldsPayload, _ := json.Marshal(WebhookPayload{App: ""})
	reqMissing := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(missingFieldsPayload))
	recMissing := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recMissing, reqMissing)
	if recMissing.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing app/image, got %d", recMissing.Code)
	}

	// GET method not allowed -> 405
	reqGet := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	recGet := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed for GET, got %d", recGet.Code)
	}
}
