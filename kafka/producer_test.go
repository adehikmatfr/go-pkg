package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return noop.NewTracerProvider().Tracer("test").Start(ctx, spanName, opts...)
}

func (noopTracer) Close() {}

func TestProducerPublishSuccess(t *testing.T) {
	cfg := mocks.NewTestConfig()
	cfg.Producer.Return.Successes = true
	mockProducer := mocks.NewSyncProducer(t, cfg)
	mockProducer.ExpectSendMessageAndSucceed()

	p := &producer{client: mockProducer, tracer: noopTracer{}}

	if err := p.Publish(context.Background(), "my-topic", []byte("hello")); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestProducerPublishFailure(t *testing.T) {
	cfg := mocks.NewTestConfig()
	cfg.Producer.Return.Successes = true
	mockProducer := mocks.NewSyncProducer(t, cfg)
	wantErr := errors.New("broker unavailable")
	mockProducer.ExpectSendMessageAndFail(wantErr)

	p := &producer{client: mockProducer, tracer: noopTracer{}}

	err := p.Publish(context.Background(), "my-topic", []byte("hello"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want %v", err, wantErr)
	}
	_ = mockProducer.Close()
}

func TestNoopPublisher(t *testing.T) {
	var pub Publisher = NoopPublisher{}

	if err := pub.Publish(context.Background(), "topic", []byte("x")); err != nil {
		t.Errorf("NoopPublisher.Publish() error = %v, want nil", err)
	}
	if err := pub.Close(); err != nil {
		t.Errorf("NoopPublisher.Close() error = %v, want nil", err)
	}
}

// publisherMock demonstrates Publisher is consumable as an interface by code
// outside this package (e.g. a usecase under test).
type publisherMock struct {
	published []string
}

func (m *publisherMock) Publish(ctx context.Context, topic string, message []byte) error {
	m.published = append(m.published, topic)
	return nil
}

func (m *publisherMock) Close() error { return nil }

func TestPublisherInterfaceIsMockable(t *testing.T) {
	var pub Publisher = &publisherMock{}
	if err := pub.Publish(context.Background(), "orders.created", []byte("{}")); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	mock := pub.(*publisherMock)
	if len(mock.published) != 1 || mock.published[0] != "orders.created" {
		t.Errorf("published = %v, want [orders.created]", mock.published)
	}
}

var _ sarama.SyncProducer = (*mocks.SyncProducer)(nil)
