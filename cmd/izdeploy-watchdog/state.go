// Package main implements the izdeploy-watchdog break-glass rescue utility.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Default filesystem paths for persistent watchdog state.
const (
	DefaultStatePath         = "/var/run/izdeploy/watchdog.state"
	DefaultFallbackStatePath = "/tmp/izdeploy-watchdog.state"
	StateDirectoryPermission = 0755
	StateFilePermission      = 0600
)

// State tracks heartbeat failure history across systemd timer runs.
type State struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	BreakGlassActive    bool      `json:"break_glass_active"`
	LastCheckTime       time.Time `json:"last_check_time"`
	LastSuccessTime     time.Time `json:"last_success_time,omitempty"`
	LastFailureTime     time.Time `json:"last_failure_time,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
}

// LoadState reads persistent state from disk or returns empty baseline state if not found.
//
// Business rule: Missing state file indicates first run or system reboot; start at zero failures.
//
// @ai-constraint: Corrupt JSON state is discarded and replaced with clean baseline to ensure resilience.
func LoadState(filePath string) *State {
	if filePath == "" {
		filePath = DefaultStatePath
	}

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return &State{}
	}

	var loadedState State
	if err := json.Unmarshal(contentBytes, &loadedState); err != nil {
		return &State{}
	}

	return &loadedState
}

// SaveState writes current state to disk atomically.
//
// Business rule: If target directory cannot be created (e.g. non-root), fall back to /tmp path.
//
// @ai-constraint: Always flush state to prevent lost failure counts on unexpected crash.
func SaveState(filePath string, state *State) (string, error) {
	if state == nil {
		return "", errors.New("watchdog: cannot persist nil state")
	}

	if filePath == "" {
		filePath = DefaultStatePath
	}

	stateDir := filepath.Dir(filePath)
	if err := os.MkdirAll(stateDir, StateDirectoryPermission); err != nil {
		filePath = DefaultFallbackStatePath
	}

	marshaled, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return filePath, err
	}

	if err := os.WriteFile(filePath, marshaled, StateFilePermission); err != nil {
		// Fallback to /tmp if primary path write fails
		if filePath != DefaultFallbackStatePath {
			filePath = DefaultFallbackStatePath
			err = os.WriteFile(filePath, marshaled, StateFilePermission)
		}
		if err != nil {
			return filePath, err
		}
	}

	return filePath, nil
}
