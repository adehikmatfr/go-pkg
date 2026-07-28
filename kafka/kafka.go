// Package kafka provides a Kafka consumer-group listener and a producer
// (see producer.go), both built on IBM/sarama.
package kafka

import (
	"context"
	"errors"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

// Tracer starts spans for consumer/producer operations; tracer.Tracer satisfies it.
type Tracer interface {
	Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span)
	Close()
}

// Config addresses the brokers to connect to and the consumer group to join.
type Config struct {
	Host      string
	Port      string
	GroupName string
}

// HandlerFunc processes a single consumed message.
type HandlerFunc func(context.Context, *sarama.ConsumerMessage)

// Consumer runs consumer-group loops against Kafka topics. Depend on this
// interface (rather than the concrete *Listener) in code that needs to be
// unit-testable with a fake/mock implementation.
type Consumer interface {
	// ListenToTopic runs a consumer group loop for topic in the background
	// until ctx is canceled or the consumer is closed.
	ListenToTopic(ctx context.Context, topic string, handle HandlerFunc)
	// Close shuts down the underlying consumer group and producer connections.
	Close() error
}

// Listener is the default Consumer implementation.
type Listener struct {
	client *Client
	tracer Tracer
}

// New builds a Listener, eagerly connecting a consumer group and producer to
// the configured brokers. Unlike pkg/postgres.New, Sarama dials brokers
// eagerly, so this call can fail — callers should be prepared to run without
// a listener (e.g. log and continue) rather than block the whole app from
// starting.
func New(cfg *Config, tracer Tracer) (Consumer, error) {
	brokers := []string{fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)}

	client, err := NewClient(brokers, cfg.GroupName)
	if err != nil {
		return nil, fmt.Errorf("kafka: %w", err)
	}

	return &Listener{client: client, tracer: tracer}, nil
}

// Client owns the underlying consumer group and producer connections.
type Client struct {
	Consumer sarama.ConsumerGroup
	Producer sarama.SyncProducer
}

// NewClient dials brokers and returns a Client with both a consumer group
// (named group) and a sync producer ready to use.
func NewClient(brokers []string, group string) (*Client, error) {
	cfg := sarama.NewConfig()
	cfg.Consumer.Offsets.AutoCommit.Enable = true
	cfg.Producer.Return.Successes = true
	cfg.Consumer.Return.Errors = true
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}

	consumer, err := sarama.NewConsumerGroup(brokers, group, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		_ = consumer.Close()
		return nil, fmt.Errorf("failed to create producer: %w", err)
	}

	return &Client{Consumer: consumer, Producer: producer}, nil
}

// Close releases the consumer group and producer. Safe to call once; the
// underlying sarama clients are not safe to close twice.
func (c *Client) Close() error {
	var errs []error
	if c.Consumer != nil {
		if err := c.Consumer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing consumer: %w", err))
		}
	}
	if c.Producer != nil {
		if err := c.Producer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing producer: %w", err))
		}
	}
	return errors.Join(errs...)
}

type consumerGroupHandler struct {
	handle HandlerFunc
	ctx    context.Context
}

func (consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (err error) {
	// A panic in the handler must not take down the whole consumer group
	// goroutine (and, with it, all other partitions this process consumes).
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("kafka: handler panicked: %v", r)
		}
	}()

	for message := range claim.Messages() {
		h.handle(h.ctx, message)
		session.MarkMessage(message, "")
	}
	return nil
}

// ListenToTopic runs a consumer group loop for topic in the background until
// ctx is canceled. handle is invoked for every message; it must not block
// indefinitely, since a slow handler stalls partition consumption. Errors
// from the consume loop are logged; the loop exits as soon as ctx is done or
// the underlying client is closed.
func (l *Listener) ListenToTopic(ctx context.Context, topic string, handle HandlerFunc) {
	go func() {
		handler := consumerGroupHandler{handle: handle, ctx: ctx}

		for {
			if ctx.Err() != nil {
				log.Info().Str("topic", topic).Msg("kafka: context canceled, stopping consumer")
				return
			}

			if err := l.client.Consumer.Consume(ctx, []string{topic}, handler); err != nil {
				if errors.Is(err, sarama.ErrClosedConsumerGroup) {
					log.Info().Str("topic", topic).Msg("kafka: consumer group closed, stopping consumer")
					return
				}
				log.Error().Err(err).Str("topic", topic).Msg("kafka: error consuming messages")
			}
		}
	}()
}

// Close shuts down the underlying client (consumer group and producer).
func (l *Listener) Close() error {
	return l.client.Close()
}
