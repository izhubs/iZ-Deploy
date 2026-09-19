// Package proxy verifies connection buffering, retry mechanisms, and sub-second zero packet loss.
package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestBufferedProxy_TransientFailureRecovery verifies that requests are buffered and retried when
// the upstream backend is temporarily returning 502 or down during container transition.
//
// Business rule: Under sub-second container swap (v1 stopping, v2 starting), incoming requests
// must be paused and retried without returning 502 to downstream clients.
//
// @ai-constraint: Ensure request bodies are cloned and retransmitted on retry attempts.
func TestBufferedProxy_TransientFailureRecovery(t *testing.T) {
	var attempts int32

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		// First 2 requests fail with 502 Bad Gateway (simulating container startup gap)
		if count < 3 {
			http.Error(w, "bad gateway (switching container)", http.StatusBadGateway)
			return
		}

		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","received":"` + string(body) + `"}`))
	}))
	defer backend.Close()

	cfg := BufferConfig{
		LBTryDuration:        1 * time.Second,
		LBTryInterval:        50 * time.Millisecond,
		MaxRequestBodySize:   1024 * 1024,
		RetriableStatusCodes: []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}

	resolver := func(req *http.Request) (string, error) {
		return backend.Listener.Addr().String(), nil
	}

	proxy := NewBufferedProxy(cfg, resolver)

	// Send POST request through proxy
	payload := `{"action":"deploy"}`
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/test", bytes.NewReader([]byte(payload)))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK after buffering, got %d", res.StatusCode)
	}

	body, _ := io.ReadAll(res.Body)
	expectedBody := `{"status":"success","received":"{"action":"deploy"}"}`
	if string(body) != expectedBody {
		t.Errorf("expected response %s, got %s", expectedBody, string(body))
	}

	metrics := proxy.GetMetrics()
	if metrics.TotalRequests != 1 {
		t.Errorf("expected 1 TotalRequests, got %d", metrics.TotalRequests)
	}
	if metrics.BufferedRetries != 1 {
		t.Errorf("expected 1 BufferedRetries, got %d", metrics.BufferedRetries)
	}
	if metrics.SuccessfulRetries != 1 {
		t.Errorf("expected 1 SuccessfulRetries, got %d", metrics.SuccessfulRetries)
	}
	if metrics.DroppedRequests != 0 {
		t.Errorf("expected 0 DroppedRequests, got %d", metrics.DroppedRequests)
	}
}

// TestBufferedProxy_MaxBodySizeExceeded verifies that requests exceeding memory limit are rejected with HTTP 413.
//
// Business rule: Protect proxy memory from exhaustion by rejecting oversized request bodies.
//
// @ai-constraint: Check StatusRequestEntityTooLarge response code.
func TestBufferedProxy_MaxBodySizeExceeded(t *testing.T) {
	cfg := BufferConfig{
		LBTryDuration:      500 * time.Millisecond,
		LBTryInterval:      50 * time.Millisecond,
		MaxRequestBodySize: 100, // 100 bytes limit for test
	}

	resolver := func(req *http.Request) (string, error) {
		return "127.0.0.1:9999", nil
	}

	proxy := NewBufferedProxy(cfg, resolver)

	largePayload := make([]byte, 200)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/upload", bytes.NewReader(largePayload))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected HTTP 413 Request Entity Too Large, got %d", res.StatusCode)
	}
}

// TestBufferedProxy_TimeoutExhaustion verifies that permanent backend failure results in 502/504
// and increments DroppedRequests counter.
//
// Business rule: If target remains down beyond LBTryDuration, proxy must terminate buffering gracefully.
//
// @ai-constraint: Keep test duration bounded (<200ms).
func TestBufferedProxy_TimeoutExhaustion(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "persistent crash", http.StatusServiceUnavailable)
	}))
	defer backend.Close()

	cfg := BufferConfig{
		LBTryDuration:        150 * time.Millisecond,
		LBTryInterval:        30 * time.Millisecond,
		MaxRequestBodySize:   1024,
		RetriableStatusCodes: []int{http.StatusServiceUnavailable},
	}

	resolver := func(req *http.Request) (string, error) {
		return backend.Listener.Addr().String(), nil
	}

	proxy := NewBufferedProxy(cfg, resolver)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/health", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("expected HTTP 502 Bad Gateway on retry exhaustion, got %d", res.StatusCode)
	}

	metrics := proxy.GetMetrics()
	if metrics.DroppedRequests != 1 {
		t.Errorf("expected 1 DroppedRequest, got %d", metrics.DroppedRequests)
	}
}
