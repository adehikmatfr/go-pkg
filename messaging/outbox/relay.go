package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/adehikmatfr/go-pkg/v2/messaging/broker"
)

// Sentinel errors returned by NewRelay.
var (
	// ErrMissingStore is returned when NewRelay is called with a nil Store.
	ErrMissingStore = errors.New("outbox: store is required")
	// ErrMissingPublisher is returned when NewRelay is called with a nil
	// Publisher.
	ErrMissingPublisher = errors.New("outbox: publisher is required")
)

// DefaultMaxAttempts bounds the number of publish attempts before an event
// is marked Dead. It can be overridden via Config.MaxAttempts.
const DefaultMaxAttempts = 10

// Config configures a Relay.
type Config struct {
	// Store is the durable outbox table. Required.
	Store Store
	// Publisher delivers a claimed event's payload. Required. Wire the same
	// messaging/broker.Publisher (or an adapter of it) the rest of the
	// service uses — same-category dependency, not a new abstraction.
	Publisher broker.Publisher
	// MaxAttempts caps publish attempts before an event goes Dead. Zero
	// uses DefaultMaxAttempts.
	MaxAttempts int
	// Backoff computes the delay before a failed event may be claimed
	// again. Nil uses DefaultBackoff.
	Backoff BackoffFunc
	// Clock stamps LastAttemptAt/NextRetryAt. Nil uses the system clock.
	Clock Clock
}

// Relay drains an outbox Store through a broker.Publisher, applying retry
// backoff and a dead-letter ceiling to events that keep failing. It owns no
// goroutine or scheduling of its own — call Drain from whatever recurring
// driver the caller already has (a ticker, scheduler/cron, a cron job), so
// this package stays a plain, synchronous operation with no cross-category
// coupling.
type Relay struct {
	store       Store
	publisher   broker.Publisher
	maxAttempts int
	backoff     BackoffFunc
	clock       Clock
}

// NewRelay constructs a Relay. Store and Publisher are required.
func NewRelay(cfg Config) (*Relay, error) {
	if cfg.Store == nil {
		return nil, ErrMissingStore
	}
	if cfg.Publisher == nil {
		return nil, ErrMissingPublisher
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	backoff := cfg.Backoff
	if backoff == nil {
		backoff = DefaultBackoff
	}
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Relay{
		store:       cfg.Store,
		publisher:   cfg.Publisher,
		maxAttempts: maxAttempts,
		backoff:     backoff,
		clock:       clock,
	}, nil
}

// DrainResult summarizes one Drain call.
type DrainResult struct {
	// Claimed is how many events ClaimPending returned.
	Claimed int
	// Published is how many were successfully published.
	Published int
	// Failed is how many failed and were rescheduled for a later attempt.
	Failed int
	// Dead is how many failed and reached the attempt ceiling this call.
	Dead int
}

// Drain claims up to limit pending events and attempts to publish each one.
// A published event is finalized via MarkPublished; a failed one has its
// RetryCount incremented and either a new NextRetryAt (via Backoff) or Dead
// set (at the attempt ceiling), then is persisted via MarkFailed. One
// event's publish failure never blocks another's.
//
// Drain does one batch and returns; call it repeatedly (from a loop or a
// recurring driver) to keep draining.
func (r *Relay) Drain(ctx context.Context, limit int) (DrainResult, error) {
	events, err := r.store.ClaimPending(ctx, limit)
	if err != nil {
		return DrainResult{}, fmt.Errorf("outbox: claim pending: %w", err)
	}

	res := DrainResult{Claimed: len(events)}
	if len(events) == 0 {
		return res, nil
	}

	var publishedIDs []string
	var failed []Event
	for _, e := range events {
		if pubErr := r.publisher.Publish(ctx, e.Topic, e.Payload); pubErr != nil {
			e.RetryCount++
			e.LastAttemptAt = r.clock.Now()
			if e.RetryCount >= r.maxAttempts {
				e.Dead = true
				res.Dead++
			} else {
				e.NextRetryAt = e.LastAttemptAt.Add(r.backoff(e.RetryCount))
				res.Failed++
			}
			failed = append(failed, e)
			continue
		}
		publishedIDs = append(publishedIDs, e.ID)
	}
	res.Published = len(publishedIDs)

	if len(publishedIDs) > 0 {
		if err := r.store.MarkPublished(ctx, publishedIDs); err != nil {
			return res, fmt.Errorf("outbox: mark published: %w", err)
		}
	}
	if len(failed) > 0 {
		if err := r.store.MarkFailed(ctx, failed); err != nil {
			return res, fmt.Errorf("outbox: mark failed: %w", err)
		}
	}
	return res, nil
}
