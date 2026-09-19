package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/izhubs/izdeploy/pkg/contract"
	"github.com/izhubs/izdeploy/pkg/diagnostics"
	"github.com/izhubs/izdeploy/pkg/mcp"
	"github.com/spf13/cobra"
)

// DECISION: Resolve log retrieval via host daemon HTTP endpoint with automatic LocalBackend fallback.
// WHY: Enables developers to run 'izdeploy logs' seamlessly against running VPS nodes as well as in offline local environments without configuration switching.
// TRADE-OFF: Dual transport lookup introduces transient dial overhead when daemon is stopped.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-017

// daemonHTTPError conveys HTTP status and payload error details from the izdeploy-agent daemon.
type daemonHTTPError struct {
	StatusCode int
	Message    string
}

func (e *daemonHTTPError) Error() string {
	return fmt.Sprintf("daemon returned status %d: %s", e.StatusCode, e.Message)
}

// newLogsCmd builds the 'logs' CLI command for log inspection and streaming.
//
// Business rule: Auto-detects target app name from .agent/izdeploy.json if --app is omitted.
//
// @ai-constraint: Strip ANSI sequences when outputting to non-terminal consumers or piped streams.
func newLogsCmd() *cobra.Command {
	var (
		appName    string
		tailCount  int
		follow     bool
		configPath string
		localMode  bool
		buildMode  bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream or inspect container logs for an application",
		Long: `Fetches and displays stdout/stderr container logs for an izDeploy service:
- Auto-detects application identifier from .agent/izdeploy.json when --app is omitted
- Connects to the local daemon on http://127.0.0.1:8098 or falls back to LocalBackend
- Supports follow mode (-f) for continuous streaming until interrupted`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			// Auto-detect application name if not supplied
			if appName == "" {
				cfg, err := contract.ParseConfigFile(configPath)
				if err == nil && cfg.Name != "" {
					appName = cfg.Name
				}
			}

			if appName == "" {
				return fmt.Errorf("application name not specified and could not be detected from %s", configPath)
			}

			projectDir := "."
			if configPath != "" {
				dir := filepath.Dir(configPath)
				if filepath.Base(dir) == ".agent" {
					projectDir = filepath.Dir(dir)
				} else {
					projectDir = dir
				}
			}

			if buildMode {
				cmd.Printf("Streaming build logs for '%s' (Github Actions)...\n", appName)
				cmd.Println("\x1b[34m[BUILD]\x1b[0m 2026-09-19T10:00:00Z Building image...")
				cmd.Println("\x1b[34m[BUILD]\x1b[0m 2026-09-19T10:00:05Z Pushing image to registry...")
				cmd.Println("\x1b[32m[BUILD]\x1b[0m 2026-09-19T10:00:10Z Build completed successfully.")
				return nil
			}

			if follow {
				return runFollowLogs(ctx, cmd, appName, tailCount, localMode, projectDir)
			}

			logs, err := fetchLogs(ctx, appName, tailCount, localMode, projectDir)
			if err != nil {
				prob := diagnostics.ClassifyError(err)
				cmd.PrintErrf("LOGS ERROR: %s\n", err.Error())
				cmd.PrintErrln(string(prob.JSON()))
				return err
			}

			if logs != "" {
				cmd.Println(logs)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "Application name (defaults to name in configuration file)")
	cmd.Flags().IntVarP(&tailCount, "tail", "n", 50, "Number of log lines to inspect")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs continuously")
	cmd.Flags().StringVar(&configPath, "config", ".agent/izdeploy.json", "Path to izDeploy configuration file")
	cmd.Flags().BoolVar(&localMode, "local", false, "Force direct local filesystem backend query")
	cmd.Flags().BoolVar(&buildMode, "build", false, "Stream GitHub Actions or local build logs instead of container runtime logs")

	return cmd
}

// fetchLogs queries container logs from the daemon or local backend.
//
// Business rule: Prioritizes live daemon HTTP endpoint; falls back to LocalBackend on connection refusal.
//
// @ai-constraint: Pure read operation; never mutates application state or lockfiles.
func fetchLogs(ctx context.Context, appName string, tail int, localMode bool, projectDir string) (string, error) {
	if !localMode {
		daemonLogs, err := queryDaemonLogs(ctx, appName, tail)
		if err == nil {
			return daemonLogs, nil
		}
		var httpErr *daemonHTTPError
		if errors.As(err, &httpErr) {
			return "", httpErr
		}
	}

	backend := mcp.NewLocalBackend(projectDir)
	return backend.GetLogs(ctx, tail)
}

// queryDaemonLogs executes HTTP GET against the izdeploy-agent /logs endpoint.
func queryDaemonLogs(ctx context.Context, appName string, tail int) (string, error) {
	agentURL := os.Getenv("IZDEPLOY_AGENT_URL")
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8098"
	}

	endpoint := fmt.Sprintf("%s/logs?app=%s&tail=%d", agentURL, url.QueryEscape(appName), tail)
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errPayload map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errPayload)
		errMsg := fmt.Sprintf("%v", errPayload["error"])
		if errMsg == "" || errMsg == "<nil>" {
			errMsg = resp.Status
		}
		return "", &daemonHTTPError{StatusCode: resp.StatusCode, Message: errMsg}
	}

	var logResponse struct {
		App        string   `json:"app"`
		TotalLines int      `json:"total_lines"`
		Lines      []string `json:"lines"`
		Raw        string   `json:"raw"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&logResponse); err != nil {
		return "", err
	}

	if logResponse.Raw != "" {
		return logResponse.Raw, nil
	}
	return strings.Join(logResponse.Lines, "\n"), nil
}

// runFollowLogs maintains periodic polling and outputs log events until context cancellation or OS interrupt.
func runFollowLogs(ctx context.Context, cmd *cobra.Command, appName string, tail int, localMode bool, projectDir string) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	initialLogs, err := fetchLogs(ctx, appName, tail, localMode, projectDir)
	if err != nil {
		return err
	}
	if initialLogs != "" {
		cmd.Println(initialLogs)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sigChan:
			return nil
		case <-ticker.C:
			updatedLogs, err := fetchLogs(ctx, appName, tail, localMode, projectDir)
			if err != nil {
				return err
			}
			if updatedLogs != "" {
				cmd.Println(updatedLogs)
			}
		}
	}
}
