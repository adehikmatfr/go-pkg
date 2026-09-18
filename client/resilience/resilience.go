// Package resilience builds a resilient http.RoundTripper for outbound HTTP
// calls: retry on transport errors and 5xx/429 responses (exponential
// backoff), a consecutive-failure circuit breaker, and a per-attempt
// timeout — composed via github.com/failsafe-go/failsafe-go. Plug the
// result into any *http.Client's Transport (including one already built by
// client/rest, or a bare net/http client); vendor types never cross this
// package's exported surface.
//
// Plain package: there is one well-known way to compose retry, breaker and
// timeout policies in Go (failsafe-go), so this stays unsplit rather than a
// port with adapters — see .assist/skills/ports-and-adapters/SKILL.md.
package resilience

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"github.com/failsafe-go/failsafe-go/timeout"
)

// Sentinel errors returned by New.
var (
	// ErrInvalidMaxRetries is returned when Config.MaxRetries is negative.
	ErrInvalidMaxRetries = errors.New("resilience: MaxRetries must be >= 0")
	// ErrInvalidFailureThreshold is returned when Config.FailureThreshold is
	// zero.
	ErrInvalidFailureThreshold = errors.New("resilience: FailureThreshold must be >= 1")
)

// Config configures the resilience policies. Every duration/threshold field
// has a documented default applied when left zero, so a caller can start
// from Config{} and only override what matters for their partner/dependency.
type Config struct {
	// MaxRetries is the number of retries (in addition to the initial
	// attempt) for a retryable failure (5xx, 429, or a transport error). 0
	// disables retries. Must be >= 0.
	MaxRetries int
	// RetryBackoff is the base delay between retries; backoff grows
	// exponentially up to RetryMaxBackoff. Defaults to 100ms when zero.
	RetryBackoff time.Duration
	// RetryMaxBackoff caps the retry backoff delay. Defaults to 2s when
	// zero.
	RetryMaxBackoff time.Duration

	// FailureThreshold is the number of consecutive failures that trip the
	// circuit breaker open. Must be >= 1.
	FailureThreshold uint
	// OpenDelay is how long the breaker stays open before probing
	// half-open. Defaults to 5s when zero.
	OpenDelay time.Duration
	// SuccessThreshold is the number of consecutive successes in half-open
	// state required to close the breaker. Defaults to 1 when zero.
	SuccessThreshold uint

	// Timeout is the per-attempt timeout. Defaults to 10s when zero.
	Timeout time.Duration
}

// validate returns a normalized copy of c with defaults applied, or an
// error describing the first invalid field.
func (c Config) validate() (Config, error) {
	if c.MaxRetries < 0 {
		return c, ErrInvalidMaxRetries
	}
	if c.FailureThreshold < 1 {
		return c, ErrInvalidFailureThreshold
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 100 * time.Millisecond
	}
	if c.RetryMaxBackoff <= 0 {
		c.RetryMaxBackoff = 2 * time.Second
	}
	if c.OpenDelay <= 0 {
		c.OpenDelay = 5 * time.Second
	}
	if c.SuccessThreshold < 1 {
		c.SuccessThreshold = 1
	}
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}
	return c, nil
}

// New builds an http.RoundTripper that wraps base with retry, a circuit
// breaker, and a per-attempt timeout. A nil base uses http.DefaultTransport.
// It returns an error wrapping ErrInvalidMaxRetries/ErrInvalidFailureThreshold
// for an invalid Config.
func New(base http.RoundTripper, cfg Config) (http.RoundTripper, error) {
	normalized, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}

	// Policies are composed around the round trip and handle responses in
	// reverse order, so the argument order is outer-most first: breaker
	// wraps retry wraps timeout wraps the transport.
	executor := failsafe.With[*http.Response](
		buildCircuitBreaker(normalized),
		buildRetryPolicy(normalized),
		timeout.New[*http.Response](normalized.Timeout),
	)

	return failsafehttp.NewRoundTripperWithExecutor(base, executor), nil
}

// buildRetryPolicy returns a retry policy that retries transport errors and
// 5xx/429 responses (via failsafehttp's default predicate) with exponential
// backoff, while never retrying context cancellation.
func buildRetryPolicy(c Config) failsafe.Policy[*http.Response] {
	return failsafehttp.NewRetryPolicyBuilder().
		WithMaxRetries(c.MaxRetries).
		WithBackoff(c.RetryBackoff, c.RetryMaxBackoff).
		AbortOnErrors(context.Canceled, context.DeadlineExceeded).
		ReturnLastFailure().
		Build()
}

// buildCircuitBreaker returns a consecutive-failure circuit breaker that
// treats transport errors and 5xx/429 responses as failures, leaving 4xx as
// successes (a 4xx is the caller's fault, not the dependency being
// unhealthy).
func buildCircuitBreaker(c Config) failsafe.Policy[*http.Response] {
	return circuitbreaker.NewBuilder[*http.Response]().
		HandleIf(func(resp *http.Response, err error) bool {
			if err != nil {
				return !errors.Is(err, context.Canceled)
			}
			if resp == nil {
				return false
			}
			return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		}).
		WithFailureThreshold(c.FailureThreshold).
		WithSuccessThreshold(c.SuccessThreshold).
		WithDelay(c.OpenDelay).
		Build()
}

// IsCircuitOpen reports whether err is (or wraps) the circuit-breaker's
// open-circuit sentinel, so a caller can fail fast without importing
// failsafe-go directly.
func IsCircuitOpen(err error) bool {
	return errors.Is(err, circuitbreaker.ErrOpen)
}

// IsTimeout reports whether err is (or wraps) the per-attempt timeout
// sentinel, so a caller can distinguish it from other transport failures
// without importing failsafe-go directly.
func IsTimeout(err error) bool {
	return errors.Is(err, timeout.ErrExceeded)
}
