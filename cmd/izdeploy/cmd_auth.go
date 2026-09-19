package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// GithubClientID represents the OAuth application ID for izDeploy.
const GithubClientID = "Iv1.8a61f9b3a7bea5b1"

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
}

// GlobalConfig represents the CLI global configuration.
type GlobalConfig struct {
	GithubToken string `json:"github_token,omitempty"`
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with GitHub using Device Flow",
		Long: `Initiates GitHub Device Flow authentication to obtain a Personal Access Token (PAT).
The token is securely stored in ~/.config/izdeploy/config.json.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Request device code
			reqBody := fmt.Sprintf(`{"client_id":"%s","scope":"repo"}`, GithubClientID)
			req, err := http.NewRequest("POST", "https://github.com/login/device/code", bytes.NewBufferString(reqBody))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to request device code: %w", err)
			}
			defer resp.Body.Close()

			var deviceResp deviceCodeResponse
			if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
				return fmt.Errorf("failed to parse device code response: %w", err)
			}

			cmd.Printf("Please open %s in your browser and enter the code: %s\n", deviceResp.VerificationURI, deviceResp.UserCode)
			cmd.Printf("Waiting for authentication...\n")

			// 2. Poll for token
			interval := time.Duration(deviceResp.Interval) * time.Second
			if interval == 0 {
				interval = 5 * time.Second
			}

			expiresAt := time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)

			for time.Now().Before(expiresAt) {
				time.Sleep(interval)

				tokenReqBody := fmt.Sprintf(`{"client_id":"%s","device_code":"%s","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`, GithubClientID, deviceResp.DeviceCode)
				tokenReq, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", bytes.NewBufferString(tokenReqBody))
				if err != nil {
					continue
				}
				tokenReq.Header.Set("Content-Type", "application/json")
				tokenReq.Header.Set("Accept", "application/json")

				tokenRespHttp, err := http.DefaultClient.Do(tokenReq)
				if err != nil {
					continue
				}

				var tokenRes tokenResponse
				if err := json.NewDecoder(tokenRespHttp.Body).Decode(&tokenRes); err != nil {
					tokenRespHttp.Body.Close()
					continue
				}
				tokenRespHttp.Body.Close()

				if tokenRes.Error == "authorization_pending" {
					continue
				}
				if tokenRes.Error == "slow_down" {
					interval += 5 * time.Second
					continue
				}
				if tokenRes.Error != "" {
					return fmt.Errorf("authentication failed: %s", tokenRes.Error)
				}

				if tokenRes.AccessToken != "" {
					// Save token to config
					if err := saveGlobalConfig(tokenRes.AccessToken); err != nil {
						return fmt.Errorf("failed to save token: %w", err)
					}
					cmd.Println("Authentication successful! Token saved to ~/.config/izdeploy/config.json.")
					return nil
				}
			}

			return fmt.Errorf("authentication timed out")
		},
	}
	return cmd
}

func saveGlobalConfig(token string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(homeDir, ".config", "izdeploy")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.json")

	var cfg GlobalConfig
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	cfg.GithubToken = token

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}
