// Package tunnel defines reverse tunnel parameters, validation rules, and configuration defaults.
package tunnel

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// Default tunnel operational parameters.
const (
	DefaultKeepAliveInterval  = 25 * time.Second
	DefaultHeartbeatInterval  = 30 * time.Second
	DefaultDialTimeout        = 10 * time.Second
	DefaultRemoteSpec         = "R:8090:127.0.0.1:8090"
	DefaultServerSchemePrefix = "https://"
)

// Configuration validation errors.
var (
	ErrEmptyServerAddress = errors.New("tunnel: server address is required")
	ErrEmptyAuthToken     = errors.New("tunnel: authentication token is required")
	ErrEmptyRemotes       = errors.New("tunnel: at least one remote port forwarding specification is required")
)

// Config encapsulates parameters required to establish an outbound reverse tunnel to the Hub.
type Config struct {
	Server             string
	AuthToken          string
	Fingerprint        string
	Remotes            []string
	KeepAlive          time.Duration
	HeartbeatInterval  time.Duration
	DialTimeout        time.Duration
	InsecureSkipVerify bool
	Headers            http.Header
	BaseBackoff        time.Duration
	MaxBackoff         time.Duration
}

// DefaultConfig returns baseline production-ready tunnel parameters.
//
// Business rule: Outbound reverse tunnels connect via TLS 443 with 25s keepalive
// to traverse strict stateful enterprise firewalls without dropping idle TCP sessions.
func DefaultConfig() Config {
	return Config{
		Server:            "",
		AuthToken:         "",
		Remotes:           []string{DefaultRemoteSpec},
		KeepAlive:         DefaultKeepAliveInterval,
		HeartbeatInterval: DefaultHeartbeatInterval,
		DialTimeout:       DefaultDialTimeout,
		Headers:           make(http.Header),
		BaseBackoff:       DefaultBaseInterval,
		MaxBackoff:        DefaultMaxInterval,
	}
}

// Validate verifies mandatory configuration invariants.
//
// Business rule: Server address, auth token, and remote forwards must be present.
//
// @ai-constraint: Auto-normalizes raw hostnames by prepending https:// if scheme is missing.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Server) == "" {
		return ErrEmptyServerAddress
	}
	if strings.TrimSpace(c.AuthToken) == "" {
		return ErrEmptyAuthToken
	}
	if len(c.Remotes) == 0 {
		return ErrEmptyRemotes
	}

	trimmedServer := strings.TrimSpace(c.Server)
	if !strings.HasPrefix(trimmedServer, "https://") && !strings.HasPrefix(trimmedServer, "http://") {
		c.Server = DefaultServerSchemePrefix + trimmedServer
	}

	if c.KeepAlive <= 0 {
		c.KeepAlive = DefaultKeepAliveInterval
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = DefaultBaseInterval
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = DefaultMaxInterval
	}
	return nil
}
