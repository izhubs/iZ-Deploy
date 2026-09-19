// Package tunnel implements secure outbound reverse tunneling and reconnect strategies.
package tunnel

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// Default backoff interval thresholds conforming to RFC 8900.
const (
	DefaultBaseInterval        = 1 * time.Second
	DefaultMaxInterval         = 60 * time.Second
	DefaultBackoffMultiplier   = 2.0
	MaxAttemptExponentCap      = 30
)

// DECISION: Adopt Full Jitter Exponential Backoff over decorrelated or pure exponential algorithms.
// WHY: RFC 8900 and empirical network studies demonstrate Full Jitter distributes reconnection
// bursts uniformly across the delay window, preventing thundering herd spikes against the Control Plane Hub.
// TRADE-OFF: Individual retry attempts may execute earlier than worst-case exponential ceilings.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-011

// FullJitterBackoff computes retry delays with randomized distribution.
type FullJitterBackoff struct {
	baseInterval time.Duration
	maxInterval  time.Duration
	multiplier   float64
	attemptCount int
	mutex        sync.Mutex
	randomSource *rand.Rand
}

// NewFullJitterBackoff initializes a thread-safe backoff calculator.
//
// Business rule: Reconnection attempts must start with a 1-second base delay
// and saturate at a 60-second ceiling to prevent network saturation.
//
// @ai-constraint: baseInterval must be positive; non-positive values fallback to DefaultBaseInterval.
func NewFullJitterBackoff(base, ceiling time.Duration, customSource ...*rand.Rand) *FullJitterBackoff {
	if base <= 0 {
		base = DefaultBaseInterval
	}
	if ceiling < base {
		ceiling = DefaultMaxInterval
	}

	var rng *rand.Rand
	if len(customSource) > 0 && customSource[0] != nil {
		rng = customSource[0]
	} else {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	return &FullJitterBackoff{
		baseInterval: base,
		maxInterval:  ceiling,
		multiplier:   DefaultBackoffMultiplier,
		attemptCount: 0,
		randomSource: rng,
	}
}

// NextDelay calculates the subsequent jittered wait duration and advances attempt counter.
//
// Business rule: Delay must satisfy 0 <= delay <= min(maxInterval, base * (multiplier ^ attempt)).
//
// @ai-constraint: Guard against integer overflow on high retry counts using MaxAttemptExponentCap.
func (b *FullJitterBackoff) NextDelay() time.Duration {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	effectiveExponent := b.attemptCount
	if effectiveExponent > MaxAttemptExponentCap {
		effectiveExponent = MaxAttemptExponentCap
	}

	ceilingFactor := math.Pow(b.multiplier, float64(effectiveExponent))
	calculatedCeiling := float64(b.baseInterval) * ceilingFactor
	maxAllowedCeiling := float64(b.maxInterval)

	if calculatedCeiling > maxAllowedCeiling {
		calculatedCeiling = maxAllowedCeiling
	}

	var randomizedDelay time.Duration
	maxNanos := int64(calculatedCeiling)
	if maxNanos <= 0 {
		randomizedDelay = b.baseInterval
	} else {
		randomizedDelay = time.Duration(b.randomSource.Int63n(maxNanos + 1))
	}

	b.attemptCount++
	return randomizedDelay
}

// Reset clears the retry attempt counter back to zero upon successful handshake.
//
// Business rule: Must be triggered immediately upon tunnel handshake validation.
//
// @ai-constraint: Thread-safe invocation via internal mutex.
func (b *FullJitterBackoff) Reset() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.attemptCount = 0
}

// AttemptCount reports the current consecutive failed attempt count.
//
// @ai-constraint: Read-only access wrapped in mutex.
func (b *FullJitterBackoff) AttemptCount() int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.attemptCount
}

// BaseInterval returns the foundational retry delay.
func (b *FullJitterBackoff) BaseInterval() time.Duration {
	return b.baseInterval
}

// MaxInterval returns the ceiling retry delay.
func (b *FullJitterBackoff) MaxInterval() time.Duration {
	return b.maxInterval
}
