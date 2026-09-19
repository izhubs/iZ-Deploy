// Package tunnel verifies reconnect backoff heuristics under synthetic load.
package tunnel

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

// TestFullJitterRange verifies returned delays strictly reside inside theoretical boundaries.
//
// Business rule: Delay must satisfy 0 <= delay <= min(maxInterval, base * (2^attempt)).
//
// @ai-constraint: Seeded RNG ensures deterministic bounds testing across runs.
func TestFullJitterRange(t *testing.T) {
	baseDuration := 1 * time.Second
	maxDuration := 60 * time.Second
	fixedSeed := int64(133742)
	testRng := rand.New(rand.NewSource(fixedSeed))

	backoff := NewFullJitterBackoff(baseDuration, maxDuration, testRng)

	for attempt := 0; attempt < 20; attempt++ {
		expectedMax := time.Duration(float64(baseDuration) * (float64(int(1) << attempt)))
		if expectedMax > maxDuration || attempt >= 6 {
			expectedMax = maxDuration
		}

		delay := backoff.NextDelay()
		if delay < 0 {
			t.Fatalf("attempt %d: negative delay received: %v", attempt, delay)
		}
		if delay > expectedMax {
			t.Fatalf("attempt %d: delay %v exceeded calculated ceiling %v", attempt, delay, expectedMax)
		}
	}
}

// TestMaxCeiling verifies retry duration never exceeds the configured max limit.
//
// Business rule: Under extreme network outages, reconnection intervals must saturate at 60 seconds.
//
// @ai-constraint: Loop through large attempt ranges to guard against exponent overflow.
func TestMaxCeiling(t *testing.T) {
	baseDuration := 1 * time.Second
	maxDuration := 60 * time.Second
	backoff := NewFullJitterBackoff(baseDuration, maxDuration)

	for attempt := 0; attempt < 100; attempt++ {
		delay := backoff.NextDelay()
		if delay > maxDuration {
			t.Fatalf("attempt %d: delay %v exceeded max ceiling %v", attempt, delay, maxDuration)
		}
	}
}

// TestReset verifies the attempt counter zeroes out on connection success.
//
// Business rule: Successful handshake restores base retry delay profile.
//
// @ai-constraint: Verify both internal counter and subsequent delay characteristics.
func TestReset(t *testing.T) {
	baseDuration := 1 * time.Second
	maxDuration := 60 * time.Second
	backoff := NewFullJitterBackoff(baseDuration, maxDuration)

	for cycle := 0; cycle < 5; cycle++ {
		backoff.NextDelay()
	}

	if backoff.AttemptCount() != 5 {
		t.Fatalf("expected attempt count 5, got %d", backoff.AttemptCount())
	}

	backoff.Reset()

	if backoff.AttemptCount() != 0 {
		t.Fatalf("expected attempt count 0 after reset, got %d", backoff.AttemptCount())
	}

	initialDelay := backoff.NextDelay()
	if initialDelay > baseDuration {
		t.Fatalf("first delay after reset %v exceeded initial base %v", initialDelay, baseDuration)
	}
}

// TestConcurrentAccess verifies thread-safety under simultaneous goroutine invocations.
//
// @ai-constraint: Run under go test -race to validate absence of data races.
func TestConcurrentAccess(t *testing.T) {
	backoff := NewFullJitterBackoff(100*time.Millisecond, 2*time.Second)
	var waitGroup sync.WaitGroup
	routineCount := 30
	iterationsPerRoutine := 50

	waitGroup.Add(routineCount)
	for workerID := 0; workerID < routineCount; workerID++ {
		go func(id int) {
			defer waitGroup.Done()
			for step := 0; step < iterationsPerRoutine; step++ {
				_ = backoff.NextDelay()
				if step%10 == 0 {
					backoff.Reset()
				}
				_ = backoff.AttemptCount()
			}
		}(workerID)
	}

	waitGroup.Wait()
}
