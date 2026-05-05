// Package resilience provides shared resilience primitives for the bakery-go
// platform: circuit breakers, exponential back-off retry, and token-bucket
// rate limiters.
package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// ---------------------------------------------------------------------------
// Circuit breaker
// ---------------------------------------------------------------------------

// State represents the current state of a circuit breaker.
type State int

const (
	StateClosed   State = iota // normal operation
	StateOpen                  // failing, reject immediately
	StateHalfOpen              // testing recovery
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit-breaker pattern for gRPC calls.
// It prevents cascading failures when a downstream service is unresponsive
// by failing fast after a threshold of consecutive errors.
//
// States:
//   - Closed: Normal. Requests pass through. Failures are counted.
//   - Open: After `failureThreshold` consecutive failures, the circuit opens.
//     Requests fail immediately without calling the downstream service.
//   - Half-Open: After `resetTimeout`, one request is allowed through to test
//     recovery. Success closes the circuit; failure reopens it.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failureCount     int
	successCount     int
	failureThreshold int
	resetTimeout     time.Duration
	lastFailure      time.Time
}

// Options configures a CircuitBreaker.
type Options struct {
	// FailureThreshold is the number of consecutive failures before opening.
	FailureThreshold int
	// ResetTimeout is how long to wait before transitioning from open to
	// half-open.
	ResetTimeout time.Duration
}

// DefaultOptions provides sensible defaults.
var DefaultOptions = Options{
	FailureThreshold: 5,
	ResetTimeout:     30 * time.Second,
}

// NewCircuitBreaker creates a circuit breaker with the given options.
func NewCircuitBreaker(opts Options) *CircuitBreaker {
	if opts.FailureThreshold <= 0 {
		opts.FailureThreshold = DefaultOptions.FailureThreshold
	}
	if opts.ResetTimeout <= 0 {
		opts.ResetTimeout = DefaultOptions.ResetTimeout
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: opts.FailureThreshold,
		resetTimeout:     opts.ResetTimeout,
	}
}

// Allow reports whether a request is permitted.
// Returns (allowed, retryAfter) — retryAfter is non-zero when the circuit
// is open and the caller should retry after that duration.
func (cb *CircuitBreaker) Allow() (allowed bool, retryAfter time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true, 0

	case StateOpen:
		since := time.Since(cb.lastFailure)
		if since >= cb.resetTimeout {
			cb.state = StateHalfOpen
			return true, 0
		}
		return false, cb.resetTimeout - since

	case StateHalfOpen:
		// Only allow one request at a time in half-open state.
		if cb.successCount > 0 {
			return false, cb.resetTimeout
		}
		return true, 0

	default:
		return false, 0
	}
}

// RecordSuccess records a successful call. If in half-open state, closes the
// circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		log.Info("circuit-breaker: half-open → closed (recovery confirmed)")
		cb.state = StateClosed
		cb.failureCount = 0
		cb.successCount = 0
	}
}

// RecordFailure records a failed call. If in half-open state, reopens the
// circuit. If in closed state and the failure threshold is reached, opens.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = time.Now()

	switch cb.state {
	case StateHalfOpen:
		log.Warn("circuit-breaker: half-open → open (recovery failed)")
		cb.state = StateOpen
		cb.failureCount++
		cb.successCount = 0

	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			log.WithField("failures", cb.failureCount).
				Warn("circuit-breaker: closed → open (threshold reached)")
			cb.state = StateOpen
			cb.successCount = 0
		}

	case StateOpen:
		cb.failureCount++
	}
}

// State returns the current circuit breaker state (for metrics/logging).
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// ---------------------------------------------------------------------------
// Retry
// ---------------------------------------------------------------------------

// RetryConfig holds parameters for the retry wrapper.
type RetryConfig struct {
	MaxRetries       int           // max attempts (0 = no retry)
	BaseDelay        time.Duration // initial delay between retries
	MaxDelay         time.Duration // maximum delay between retries
	Multiplier       float64       // delay multiplier per retry
	CircuitBreaker   *CircuitBreaker
}

// DefaultRetryConfig provides sensible defaults.
var DefaultRetryConfig = RetryConfig{
	MaxRetries:   3,
	BaseDelay:    100 * time.Millisecond,
	MaxDelay:     2 * time.Second,
	Multiplier:   2.0,
	CircuitBreaker: nil, // set per-endpoint
}

// RetryFn is the function type accepted by Retry.
type RetryFn func(context.Context) error

// Retry executes fn with exponential-backoff retries and circuit-breaker
// checks. Returns the result of the last attempt if all retries fail.
func Retry(ctx context.Context, cfg RetryConfig, fn RetryFn) error {
	var lastErr error
	delay := cfg.BaseDelay

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Check circuit breaker before each attempt.
		if cfg.CircuitBreaker != nil {
			allowed, retryAfter := cfg.CircuitBreaker.Allow()
			if !allowed {
				log.WithField("retry_after", retryAfter).
					Warnf("circuit-breaker open, skipping attempt %d", attempt+1)
				return fmt.Errorf("circuit breaker open, retry after %v", retryAfter)
			}
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			if cfg.CircuitBreaker != nil {
				cfg.CircuitBreaker.RecordSuccess()
			}
			return nil
		}

		// Record failure for circuit breaker.
		if cfg.CircuitBreaker != nil {
			cfg.CircuitBreaker.RecordFailure()
		}

		if attempt < cfg.MaxRetries {
			log.WithFields(log.Fields{
				"attempt": attempt + 1,
				"error":   lastErr,
			}).Warn("retrying downstream call")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			delay = time.Duration(float64(delay) * cfg.Multiplier)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		}
	}

	return fmt.Errorf("all %d retries exhausted: %w", cfg.MaxRetries+1, lastErr)
}

// ---------------------------------------------------------------------------
// Token-bucket rate limiter
// ---------------------------------------------------------------------------

// TokenBucket implements a fixed-window token-bucket rate limiter.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// NewTokenBucket creates a bucket that refills at `rate` tokens/sec with a
// maximum burst of `burst` tokens.
func NewTokenBucket(rate float64, burst float64) *TokenBucket {
	return &TokenBucket{
		tokens:     burst,
		maxTokens:  burst,
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

// Allow reports whether one token is available.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// RateLimiter maps per-identity token buckets and provides thread-safe
// rate-limit checks.
type RateLimiter struct {
	mu       sync.Mutex
	limiter  map[string]*TokenBucket
	defaultR float64 // default rate (req/s)
	defaultB float64 // default burst
}

// NewRateLimiter creates a new rate limiter. A per-identity bucket is
// lazily created on first use.
func NewRateLimiter(defaultRate float64, defaultBurst float64) *RateLimiter {
	return &RateLimiter{
		limiter:  make(map[string]*TokenBucket),
		defaultR: defaultRate,
		defaultB: defaultBurst,
	}
}

// Allow checks whether the given identity is permitted to proceed.
func (rl *RateLimiter) Allow(id string) bool {
	rl.mu.Lock()
	tb, exists := rl.limiter[id]
	if !exists {
		tb = NewTokenBucket(rl.defaultR, rl.defaultB)
		rl.limiter[id] = tb
	}
	rl.mu.Unlock()
	return tb.Allow()
}
