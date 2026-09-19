// Package docker_test verifies log parsing, ANSI code stripping, and bounded ring buffer dynamics.
package docker_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/izhubs/izdeploy/pkg/docker"
)

func TestStripANSI(t *testing.T) {
	coloredInput := "\x1b[31mError:\x1b[0m failed connecting to database \x1b[32m[OK]\x1b[0m\r\n"
	expectedOutput := "Error: failed connecting to database [OK]\r\n"

	result := docker.StripANSI(coloredInput)
	if result != expectedOutput {
		t.Errorf("expected %q, got %q", expectedOutput, result)
	}
}

func TestRingBuffer_SmallInput(t *testing.T) {
	rb := docker.NewRingBuffer(15, 45)

	for i := 1; i <= 10; i++ {
		rb.AddLine(fmt.Sprintf("log line %d", i))
	}

	lines := rb.Lines()
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(lines))
	}
	if lines[0] != "log line 1" || lines[9] != "log line 10" {
		t.Errorf("unexpected line ordering: %+v", lines)
	}
	if rb.TotalCount() != 10 {
		t.Errorf("expected total count 10, got %d", rb.TotalCount())
	}
}

func TestRingBuffer_ExceedsCapacity(t *testing.T) {
	rb := docker.NewRingBuffer(15, 45)

	const totalLogs = 100
	for i := 1; i <= totalLogs; i++ {
		rb.AddLine(fmt.Sprintf("entry %d", i))
	}

	lines := rb.Lines()
	// 15 head + 1 omission notice + 45 tail = 61 items
	const expectedRenderedCount = 61
	if len(lines) != expectedRenderedCount {
		t.Fatalf("expected %d rendered lines, got %d", expectedRenderedCount, len(lines))
	}

	// Verify head
	if lines[0] != "entry 1" || lines[14] != "entry 15" {
		t.Errorf("head lines corrupted: %q, %q", lines[0], lines[14])
	}

	// Verify omission notice
	expectedNotice := "[... 40 lines omitted ...]"
	if lines[15] != expectedNotice {
		t.Errorf("expected notice %q, got %q", expectedNotice, lines[15])
	}

	// Verify tail
	if lines[16] != "entry 56" || lines[60] != "entry 100" {
		t.Errorf("tail lines corrupted: %q, %q", lines[16], lines[60])
	}

	if rb.TotalCount() != totalLogs {
		t.Errorf("expected total count %d, got %d", totalLogs, rb.TotalCount())
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := docker.NewRingBuffer(15, 45)
	var wg sync.WaitGroup

	const goroutines = 10
	const linesPerWorker = 50

	for workerID := 0; workerID < goroutines; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < linesPerWorker; i++ {
				rb.AddLine(fmt.Sprintf("worker %d log %d", id, i))
				_ = rb.Lines()
			}
		}(workerID)
	}

	wg.Wait()
	if rb.TotalCount() != goroutines*linesPerWorker {
		t.Errorf("expected %d total lines, got %d", goroutines*linesPerWorker, rb.TotalCount())
	}
}
