package kafka

import (
	"context"

	"github.com/IBM/sarama"

	"github.com/adehikmatfr/go-pkg/v2/messaging/broker"
)

type producer struct {
	client sarama.SyncProducer
	tracer Tracer
}

// NewProducer opens a producer-only Sarama connection (no consumer group is
// joined), returning a broker.Publisher. Sarama dials the brokers eagerly,
// so this call can fail — callers should fall back to broker.NoopPublisher
// when Kafka isn't reachable rather than block the whole app from starting.
func NewProducer(brokers []string, tracer Tracer) (broker.Publisher, error) { // coverage-ignore
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true

	client, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, err
	}

	return NewPublisher(client, tracer), nil
}

// NewPublisher wraps an already-connected sarama.SyncProducer as a
// broker.Publisher. Most callers should use NewProducer, which also dials
// the brokers; NewPublisher is for sharing one producer across publishers,
// or supplying a fake/mock producer (e.g. github.com/IBM/sarama/mocks) in a
// test.
func NewPublisher(client sarama.SyncProducer, tracer Tracer) broker.Publisher {
	return &producer{client: client, tracer: tracer}
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
