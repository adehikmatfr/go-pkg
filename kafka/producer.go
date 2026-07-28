package kafka

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/rs/zerolog/log"
)

// Publisher is the narrow interface callers need to emit events — small
// enough to fake in tests and to no-op when Kafka isn't reachable at
// startup (see NoopPublisher).
type Publisher interface {
	Publish(ctx context.Context, topic string, message []byte) error
	// Close releases the underlying connection. Call it during shutdown.
	Close() error
}

type producer struct {
	client sarama.SyncProducer
	tracer Tracer
}

// NewProducer opens a producer-only Sarama connection (no consumer group is
// joined). Sarama dials the brokers eagerly, so this call can fail — callers
// should fall back to NoopPublisher when Kafka isn't reachable rather than
// block the whole app from starting.
func NewProducer(brokers []string, tracer Tracer) (Publisher, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true

	client, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, err
	}

	return &producer{client: client, tracer: tracer}, nil
}

func (p *producer) Publish(ctx context.Context, topic string, message []byte) error {
	_, span := p.tracer.Start(ctx, "pkg.kafka.producer/Publish")
	defer span.End()

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(message),
	}

	_, _, err := p.client.SendMessage(msg)
	return err
}

// Close releases the underlying producer connection.
func (p *producer) Close() error {
	return p.client.Close()
}

// NoopPublisher drops events with a warning log instead of erroring — used
// as a fallback when the Kafka broker is unreachable at startup, so the rest
// of the app can still run.
type NoopPublisher struct{}

func (NoopPublisher) Publish(ctx context.Context, topic string, message []byte) error {
	log.Warn().Str("topic", topic).Msg("kafka: publisher unavailable, dropping event")
	return nil
}

func (NoopPublisher) Close() error { return nil }
