package broker_test

import (
	"context"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/messaging/broker"
)

func TestNoopPublisher(t *testing.T) {
	var pub broker.Publisher = broker.NoopPublisher{}

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
	var pub broker.Publisher = &publisherMock{}
	if err := pub.Publish(context.Background(), "orders.created", []byte("{}")); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	mock := pub.(*publisherMock)
	if len(mock.published) != 1 || mock.published[0] != "orders.created" {
		t.Errorf("published = %v, want [orders.created]", mock.published)
	}
}
