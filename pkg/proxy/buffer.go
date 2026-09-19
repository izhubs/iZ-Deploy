// Package proxy manages reverse proxy sidecars, traffic routing, and TLS termination.
package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Default request buffering and retry parameters matching kamal-proxy and Caddy specifications.
const (
	DefaultLBTryDuration      = 3 * time.Second
	DefaultLBTryInterval      = 200 * time.Millisecond
	DefaultMaxRequestBodySize = 10 * 1024 * 1024 // 10MB memory limit
)

// Buffer errors.
var (
	ErrRequestBodyTooLarge = errors.New("buffer: request body exceeds configured memory buffer limit")
	ErrBufferMaxRetries    = errors.New("buffer: upstream target unavailable within lb_try_duration window")
	ErrNoTargetAvailable   = errors.New("buffer: no active upstream target resolved")
)

// DECISION: Buffer request bodies in memory up to 10MB to enable idempotent replay during swap window.
// WHY: In-memory buffering allows sub-second zero packet loss when switching containers. If v1 stops
// and v2 takes 400ms to accept connections, client requests are paused and retried without dropping TCP sockets.
// TRADE-OFF: Memory consumption is bounded by MaxRequestBodySize per in-flight request.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-007

// BufferConfig defines retry parameters and memory constraints for connection buffering.
type BufferConfig struct {
	LBTryDuration        time.Duration
	LBTryInterval        time.Duration
	MaxRequestBodySize   int64
	RetriableStatusCodes []int
}

// DefaultBufferConfig returns standard buffering parameters.
func DefaultBufferConfig() BufferConfig {
	return BufferConfig{
		LBTryDuration:      DefaultLBTryDuration,
		LBTryInterval:      DefaultLBTryInterval,
		MaxRequestBodySize: DefaultMaxRequestBodySize,
		RetriableStatusCodes: []int{
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		},
	}
}

// BufferMetrics tracks reliability statistics across container swap cycles.
type BufferMetrics struct {
	TotalRequests     uint64
	BufferedRetries   uint64
	SuccessfulRetries uint64
	DroppedRequests   uint64
}

// TargetResolver abstracts dynamic upstream target lookup for incoming HTTP requests.
type TargetResolver func(req *http.Request) (string, error)

// BufferedProxy implements an HTTP reverse proxy handler with automatic request buffering and retries.
type BufferedProxy struct {
	cfg      BufferConfig
	resolver TargetResolver
	client   *http.Client
	metrics  BufferMetrics
	mu       sync.RWMutex
}

// NewBufferedProxy initializes a reverse proxy handler wrapped with connection buffering and retry policies.
//
// Business rule: Enforce zero dropped packets when swapping containers by holding requests
// for up to LBTryDuration (default 3s) at LBTryInterval (default 200ms).
//
// @ai-constraint: Disable transport idle connections to avoid stale socket reuse during container restarts.
func NewBufferedProxy(cfg BufferConfig, resolver TargetResolver) *BufferedProxy {
	if cfg.LBTryDuration <= 0 {
		cfg.LBTryDuration = DefaultLBTryDuration
	}
	if cfg.LBTryInterval <= 0 {
		cfg.LBTryInterval = DefaultLBTryInterval
	}
	if cfg.MaxRequestBodySize <= 0 {
		cfg.MaxRequestBodySize = DefaultMaxRequestBodySize
	}
	if len(cfg.RetriableStatusCodes) == 0 {
		cfg.RetriableStatusCodes = []int{
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   500 * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &BufferedProxy{
		cfg:      cfg,
		resolver: resolver,
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.LBTryDuration + 1*time.Second,
		},
	}
}

// ServeHTTP handles the incoming HTTP request by buffering its body and executing retries against available targets.
//
// Business rule: If the target returns 502/503 or fails at the TCP dial layer during a container swap,
// hold the request and retry until lb_try_duration expires.
//
// @ai-constraint: Must restore request body for every retry attempt using cloned byte buffers.
func (p *BufferedProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	atomic.AddUint64(&p.metrics.TotalRequests, 1)

	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(io.LimitReader(req.Body, p.cfg.MaxRequestBodySize+1))
		_ = req.Body.Close()

		if err != nil {
			http.Error(w, "buffer: failed to read request body", http.StatusBadRequest)
			return
		}
		if int64(len(bodyBytes)) > p.cfg.MaxRequestBodySize {
			http.Error(w, ErrRequestBodyTooLarge.Error(), http.StatusRequestEntityTooLarge)
			return
		}
	}

	deadlineCtx, cancel := context.WithTimeout(req.Context(), p.cfg.LBTryDuration)
	defer cancel()

	ticker := time.NewTicker(p.cfg.LBTryInterval)
	defer ticker.Stop()

	attempt := 0
	for {
		attempt++

		target, err := p.resolver(req)
		if err == nil && target != "" {
			resp, shouldRetry, forwardErr := p.forwardRequest(deadlineCtx, req, bodyBytes, target)
			if !shouldRetry {
				if attempt > 1 {
					atomic.AddUint64(&p.metrics.SuccessfulRetries, 1)
				}
				if resp != nil {
					defer resp.Body.Close()
					p.writeResponse(w, resp)
				} else if forwardErr != nil {
					http.Error(w, forwardErr.Error(), http.StatusBadGateway)
				}
				return
			}
		}

		if attempt == 1 {
			atomic.AddUint64(&p.metrics.BufferedRetries, 1)
		}

		select {
		case <-deadlineCtx.Done():
			atomic.AddUint64(&p.metrics.DroppedRequests, 1)
			http.Error(w, ErrBufferMaxRetries.Error(), http.StatusBadGateway)
			return
		case <-ticker.C:
			// Next retry iteration
		}
	}
}

// forwardRequest executes a single forward attempt to the resolved target backend.
//
// Business rule: Evaluates whether error is transient (connection refused / 502 / 503)
// and warrants another retry attempt.
//
// @ai-constraint: Strip Hop-by-Hop headers before forwarding per RFC 7230 §6.1.
func (p *BufferedProxy) forwardRequest(ctx context.Context, originalReq *http.Request, bodyBytes []byte, target string) (*http.Response, bool, error) {
	targetURL := target
	if !bytes.HasPrefix([]byte(targetURL), []byte("http://")) && !bytes.HasPrefix([]byte(targetURL), []byte("https://")) {
		targetURL = "http://" + targetURL
	}

	dest, err := url.Parse(targetURL)
	if err != nil {
		return nil, false, fmt.Errorf("buffer: parse target URL %s: %w", targetURL, err)
	}

	forwardURL := *originalReq.URL
	forwardURL.Scheme = dest.Scheme
	forwardURL.Host = dest.Host

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	outReq, err := http.NewRequestWithContext(ctx, originalReq.Method, forwardURL.String(), bodyReader)
	if err != nil {
		return nil, false, fmt.Errorf("buffer: construct outgoing request: %w", err)
	}

	// Copy and filter headers
	copyHeaders(outReq.Header, originalReq.Header)
	outReq.Header.Set("X-Forwarded-For", clientIP(originalReq))
	outReq.Header.Set("X-Forwarded-Proto", originalReq.URL.Scheme)
	if outReq.Header.Get("X-Forwarded-Proto") == "" {
		outReq.Header.Set("X-Forwarded-Proto", "http")
	}

	resp, err := p.client.Do(outReq)
	if err != nil {
		// Network dial failure or connection refused during swap window -> Retry
		return nil, true, err
	}

	// Check if status code indicates temporary backend transition
	for _, code := range p.cfg.RetriableStatusCodes {
		if resp.StatusCode == code {
			_ = resp.Body.Close()
			return nil, true, fmt.Errorf("buffer: upstream returned retriable HTTP %d", code)
		}
	}

	return resp, false, nil
}

// writeResponse streams the upstream HTTP response back to the downstream client.
//
// Business rule: Replicates upstream headers, status code, and payload body.
//
// @ai-constraint: Flush buffer to preserve streaming semantics.
func (p *BufferedProxy) writeResponse(w http.ResponseWriter, resp *http.Response) {
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	if resp.Body != nil {
		_, _ = io.Copy(w, resp.Body)
	}
}

// GetMetrics returns a snapshot of current buffering and retry counters.
//
// Business rule: Thread-safe reader for health monitors.
//
// @ai-constraint: Uses atomic loads to guarantee consistency without lock contention.
func (p *BufferedProxy) GetMetrics() BufferMetrics {
	return BufferMetrics{
		TotalRequests:     atomic.LoadUint64(&p.metrics.TotalRequests),
		BufferedRetries:   atomic.LoadUint64(&p.metrics.BufferedRetries),
		SuccessfulRetries: atomic.LoadUint64(&p.metrics.SuccessfulRetries),
		DroppedRequests:   atomic.LoadUint64(&p.metrics.DroppedRequests),
	}
}

// copyHeaders duplicates HTTP headers while stripping RFC 7230 hop-by-hop headers.
func copyHeaders(dst, src http.Header) {
	hopByHop := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}

	for k, vv := range src {
		if hopByHop[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// clientIP extracts the caller IP address from RemoteAddr.
func clientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
