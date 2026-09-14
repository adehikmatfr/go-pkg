package kafka_test

import (
	"context"
	"errors"
	"testing"

	"github.com/IBM/sarama/mocks"

	"github.com/adehikmatfr/go-pkg/v2/messaging/broker/kafka"
)

func TestPublisherPublishSuccess(t *testing.T) {
	cfg := mocks.NewTestConfig()
	cfg.Producer.Return.Successes = true
	mockProducer := mocks.NewSyncProducer(t, cfg)
	mockProducer.ExpectSendMessageAndSucceed()

	pub := kafka.NewPublisher(mockProducer, expectTracerStart(t))

	if err := pub.Publish(context.Background(), "my-topic", []byte("hello")); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestPublisherPublishFailure(t *testing.T) {
	cfg := mocks.NewTestConfig()
	cfg.Producer.Return.Successes = true
	mockProducer := mocks.NewSyncProducer(t, cfg)
	wantErr := errors.New("broker unavailable")
	mockProducer.ExpectSendMessageAndFail(wantErr)

	pub := kafka.NewPublisher(mockProducer, expectTracerStart(t))

	err := pub.Publish(context.Background(), "my-topic", []byte("hello"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want %v", err, wantErr)
	}
	_ = mockProducer.Close()
}
