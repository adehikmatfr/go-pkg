// Package broker defines the Publisher/Consumer ports for asynchronous
// messaging, independent of which broker actually moves the bytes. Concrete
// brokers live in subpackages (e.g. broker/kafka) that implement these
// interfaces — swapping the broker is a one-line constructor change for the
// consumer, nothing else.
package broker

import (
	"context"

	"github.com/rs/zerolog/log"
)

// Message is a broker-agnostic representation of one consumed message. Every
// adapter translates its own wire/library type (e.g. sarama.ConsumerMessage)
// into this before invoking HandlerFunc, so handler code never imports a
// specific broker's client library.
type Message struct {
	Topic string
	Key   []byte
	Value []byte
}

// HandlerFunc processes a single consumed message.
type HandlerFunc func(context.Context, Message)

// Consumer runs consumer-group/queue listen loops against topics. Depend on
// this interface (rather than a concrete adapter type) in code that needs to
// be unit-testable with a fake/mock implementation.
type Consumer interface {
	// ListenToTopic runs a listen loop for topic in the background until ctx
	// is canceled or the consumer is closed.
	ListenToTopic(ctx context.Context, topic string, handle HandlerFunc)
	// Close shuts down the underlying consumer connection(s).
	Close() error
}

// Publisher emits a message to a topic. Depend on this interface (rather
// than a concrete adapter type) in code that needs to be unit-testable, or
// that should keep running via NoopPublisher when the broker is unreachable.
type Publisher interface {
	Publish(ctx context.Context, topic string, message []byte) error
	// Close releases the underlying connection. Call it during shutdown.
	Close() error
}

// NoopPublisher drops events with a warning log instead of erroring — used
// as a fallback when the broker is unreachable at startup, so the rest of
// the app can still run.
type NoopPublisher struct{}

// Publish logs a warning and drops the event; see the package doc for why
// this exists.
func (NoopPublisher) Publish(_ context.Context, topic string, _ []byte) error {
	log.Warn().Str("topic", topic).Msg("broker: publisher unavailable, dropping event")
	return nil
}

// Close is a no-op.
func (NoopPublisher) Close() error { return nil }
