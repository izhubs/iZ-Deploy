// Package docker provides an enterprise-grade Go SDK wrapper around the Docker Engine API.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// Default buffer size limits for MCP and diagnostics inspection.
const (
	DefaultHeadLinesCap = 15
	DefaultTailLinesCap = 45
	DefaultMaxLinesCap  = DefaultHeadLinesCap + DefaultTailLinesCap
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?(\x07|\x1b\\)`)

// StripANSI eliminates terminal color and cursor codes from raw container log streams.
//
// Business rule: Diagnostic and MCP tool outputs must be clean plain text to avoid LLM token distortion.
func StripANSI(rawText string) string {
	return ansiRegex.ReplaceAllString(rawText, "")
}

// RingBuffer retains a fixed sample of early bootstrap logs (head) and late runtime logs (tail).
//
// Business rule: When logs exceed capacity, summarize the middle omission to retain failure context
// within strict token limits.
type RingBuffer struct {
	headCap    int
	tailCap    int
	headLines  []string
	tailLines  []string
	totalLines int
	mutex      sync.RWMutex
}

// NewRingBuffer allocates an initialized log ring buffer.
func NewRingBuffer(headCap, tailCap int) *RingBuffer {
	if headCap <= 0 {
		headCap = DefaultHeadLinesCap
	}
	if tailCap <= 0 {
		tailCap = DefaultTailLinesCap
	}
	return &RingBuffer{
		headCap:   headCap,
		tailCap:   tailCap,
		headLines: make([]string, 0, headCap),
		tailLines: make([]string, 0, tailCap),
	}
}

// AddLine inserts a log entry, maintaining head preservation and tail FIFO sliding window.
func (rb *RingBuffer) AddLine(line string) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	cleaned := strings.TrimRight(StripANSI(line), "\r\n")
	rb.totalLines++

	if len(rb.headLines) < rb.headCap {
		rb.headLines = append(rb.headLines, cleaned)
		return
	}

	if len(rb.tailLines) < rb.tailCap {
		rb.tailLines = append(rb.tailLines, cleaned)
		return
	}

	// Slide tail window (FIFO drop oldest tail)
	copy(rb.tailLines, rb.tailLines[1:])
	rb.tailLines[len(rb.tailLines)-1] = cleaned
}

// Lines returns the combined head, omission notice (if applicable), and tail lines.
func (rb *RingBuffer) Lines() []string {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	maxCapacity := rb.headCap + rb.tailCap
	if rb.totalLines <= maxCapacity {
		result := make([]string, 0, len(rb.headLines)+len(rb.tailLines))
		result = append(result, rb.headLines...)
		result = append(result, rb.tailLines...)
		return result
	}

	omittedCount := rb.totalLines - maxCapacity
	result := make([]string, 0, maxCapacity+1)
	result = append(result, rb.headLines...)
	result = append(result, fmt.Sprintf("[... %d lines omitted ...]", omittedCount))
	result = append(result, rb.tailLines...)
	return result
}

// String renders the sampled log stream joined with newline characters.
func (rb *RingBuffer) String() string {
	return strings.Join(rb.Lines(), "\n")
}

// TotalCount returns the absolute count of ingested lines including omitted ones.
func (rb *RingBuffer) TotalCount() int {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return rb.totalLines
}

// DECISION: Demultiplex Docker logs via stdcopy with plain stream fallback.
// WHY: Containers without TTY emit 8-byte multiplexed headers; TTY containers emit raw bytes.
// TRADE-OFF: Double-pass check on stream format if stdcopy encounters plain text.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-009

// GetContainerLogs extracts and samples container logs into a bounded RingBuffer.
//
// Business rule: Stream must close on context cancellation without goroutine leaks.
//
// @ai-constraint: Never read entire log streams indefinitely into memory; apply tail constraints.
func (c *Client) GetContainerLogs(ctx context.Context, containerID string, tailCount int) (*RingBuffer, error) {
	tailParam := "all"
	if tailCount > 0 {
		tailParam = fmt.Sprintf("%d", tailCount)
	}

	logOpts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
		Tail:       tailParam,
	}

	readCloser, err := c.cli.ContainerLogs(ctx, containerID, logOpts)
	if err != nil {
		return nil, fmt.Errorf("failed fetching logs for %s: %w", containerID, err)
	}
	defer func() { _ = readCloser.Close() }()

	return parseLogStream(readCloser)
}

func parseLogStream(reader io.Reader) (*RingBuffer, error) {
	ringBuffer := NewRingBuffer(DefaultHeadLinesCap, DefaultTailLinesCap)
	var stdoutBuf, stderrBuf bytes.Buffer

	// Attempt demultiplexing
	rawBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed reading log stream: %w", err)
	}

	_, copyErr := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, bytes.NewReader(rawBytes))
	var combinedText string
	if copyErr == nil && (stdoutBuf.Len() > 0 || stderrBuf.Len() > 0) {
		combinedText = stdoutBuf.String() + stderrBuf.String()
	} else {
		// Fallback for raw streams (TTY or non-multiplexed)
		combinedText = string(rawBytes)
	}

	scannerLines := strings.Split(combinedText, "\n")
	for _, rawLine := range scannerLines {
		if rawLine == "" && len(scannerLines) > 1 {
			continue
		}
		ringBuffer.AddLine(rawLine)
	}

	return ringBuffer, nil
}
