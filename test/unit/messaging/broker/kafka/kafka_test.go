package kafka_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/adehikmatfr/go-pkg/v2/messaging/broker"
	"github.com/adehikmatfr/go-pkg/v2/messaging/broker/kafka"
	kafkamock "github.com/adehikmatfr/go-pkg/v2/test/mock/messaging/broker/kafka"
)

// expectTracerStart returns a Tracer mock that delegates any Start call to a
// real no-op OpenTelemetry tracer. It sets no expectation count — some code
// paths under test (ListenToTopic) never call the tracer at all.
func expectTracerStart(t *testing.T) *kafkamock.Tracer {
	t.Helper()
	tr := kafkamock.NewTracer(t)
	tr.EXPECT().Start(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
			return noop.NewTracerProvider().Tracer("test").Start(ctx, name, opts...)
		},
	).Maybe()
	return tr
}

// fakeConsumerGroup implements sarama.ConsumerGroup by overriding only
// Close/Consume; every other method is unused by these tests and would
// panic on the nil embedded interface if called.
type fakeConsumerGroup struct {
	sarama.ConsumerGroup
	closeErr  error
	onConsume func(call int, handler sarama.ConsumerGroupHandler) error
	calls     int32
}

func (f *fakeConsumerGroup) Close() error { return f.closeErr }

func (f *fakeConsumerGroup) Consume(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	n := int(atomic.AddInt32(&f.calls, 1))
	return f.onConsume(n, handler)
}

// fakeSyncProducer implements sarama.SyncProducer by overriding only Close.
type fakeSyncProducer struct {
	sarama.SyncProducer
	closeErr error
}

func (f *fakeSyncProducer) Close() error { return f.closeErr }

// fakeClaim implements sarama.ConsumerGroupClaim over a fixed message channel.
type fakeClaim struct {
	messages chan *sarama.ConsumerMessage
}

func (f *fakeClaim) Topic() string                            { return "test-topic" }
func (f *fakeClaim) Partition() int32                         { return 0 }
func (f *fakeClaim) InitialOffset() int64                     { return 0 }
func (f *fakeClaim) HighWaterMarkOffset() int64               { return 0 }
func (f *fakeClaim) Messages() <-chan *sarama.ConsumerMessage { return f.messages }

// fakeSession implements sarama.ConsumerGroupSession, recording MarkMessage calls.
type fakeSession struct {
	marked int32
}

func (f *fakeSession) Claims() map[string][]int32 { return nil }
func (f *fakeSession) MemberID() string           { return "member" }
func (f *fakeSession) GenerationID() int32        { return 1 }
func (f *fakeSession) MarkOffset(topic string, partition int32, offset int64, metadata string) {
}
func (f *fakeSession) Commit() {}
func (f *fakeSession) ResetOffset(topic string, partition int32, offset int64, metadata string) {
}
func (f *fakeSession) MarkMessage(msg *sarama.ConsumerMessage, metadata string) {
	atomic.AddInt32(&f.marked, 1)
}
func (f *fakeSession) Context() context.Context { return context.Background() }

func waitForCalls(t *testing.T, calls *int32, want int32) {
	t.Helper()
	deadline := time.After(time.Second)
	for atomic.LoadInt32(calls) < want {
		select {
		case <-deadline:
			t.Fatalf("Consume calls = %d, want >= %d within 1s", atomic.LoadInt32(calls), want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestListenToTopicProcessesMessagesThenStopsOnClosedConsumerGroup drives the
// whole ListenToTopic/consumerGroupHandler flow through the sarama
// interfaces: Setup/ConsumeClaim/Cleanup are all unexported internals,
// reached here only via the sarama.ConsumerGroupHandler passed into our fake
// ConsumerGroup's Consume method.
func TestListenToTopicProcessesMessagesThenStopsOnClosedConsumerGroup(t *testing.T) {
	claim := &fakeClaim{messages: make(chan *sarama.ConsumerMessage, 2)}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte("1")}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte("2")}
	close(claim.messages)
	session := &fakeSession{}

	var handled int32
	var setupErr, claimErr, cleanupErr error

	cg := &fakeConsumerGroup{
		onConsume: func(call int, handler sarama.ConsumerGroupHandler) error {
			if call == 1 {
				setupErr = handler.Setup(session)
				claimErr = handler.ConsumeClaim(session, claim)
				cleanupErr = handler.Cleanup(session)
			}
			return sarama.ErrClosedConsumerGroup
		},
	}

	consumer := kafka.NewConsumer(&kafka.Client{Consumer: cg}, expectTracerStart(t))
	consumer.ListenToTopic(context.Background(), "topic", func(ctx context.Context, msg broker.Message) {
		atomic.AddInt32(&handled, 1)
	})

	waitForCalls(t, &cg.calls, 1)
	time.Sleep(20 * time.Millisecond) // give the loop a chance to over-call if it doesn't stop

	if setupErr != nil {
		t.Errorf("Setup() error = %v, want nil", setupErr)
	}
	if cleanupErr != nil {
		t.Errorf("Cleanup() error = %v, want nil", cleanupErr)
	}
	if claimErr != nil {
		t.Errorf("ConsumeClaim() error = %v, want nil", claimErr)
	}
	if got := atomic.LoadInt32(&handled); got != 2 {
		t.Errorf("handled = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&session.marked); got != 2 {
		t.Errorf("marked = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&cg.calls); got != 1 {
		t.Errorf("Consume calls = %d, want exactly 1 (loop must stop on ErrClosedConsumerGroup)", got)
	}
}

// TestListenToTopicRecoversHandlerPanic ensures a panicking handler surfaces
// as an error from ConsumeClaim instead of crashing the consumer goroutine
// (which would otherwise crash the whole test binary).
func TestListenToTopicRecoversHandlerPanic(t *testing.T) {
	claim := &fakeClaim{messages: make(chan *sarama.ConsumerMessage, 1)}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte("boom")}
	close(claim.messages)
	session := &fakeSession{}

	var claimErr error
	done := make(chan struct{})

	cg := &fakeConsumerGroup{
		onConsume: func(call int, handler sarama.ConsumerGroupHandler) error {
			if call == 1 {
				claimErr = handler.ConsumeClaim(session, claim)
				close(done)
			}
			return sarama.ErrClosedConsumerGroup
		},
	}

	consumer := kafka.NewConsumer(&kafka.Client{Consumer: cg}, expectTracerStart(t))
	consumer.ListenToTopic(context.Background(), "topic", func(context.Context, broker.Message) {
		panic("handler exploded")
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ConsumeClaim was never invoked")
	}

	if claimErr == nil {
		t.Fatal("ConsumeClaim() should return an error when the handler panics, not crash the goroutine")
	}
}

func TestListenToTopicStopsOnContextCancel(t *testing.T) {
	// A pre-canceled context must make the consume loop return via ctx.Err()
	// before it ever touches the ConsumerGroup — regression test for a
	// previous version that busy-looped (and could nil-deref) once the
	// consumer was closed out from under it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	consumer := kafka.NewConsumer(&kafka.Client{}, expectTracerStart(t))
	consumer.ListenToTopic(ctx, "topic", func(context.Context, broker.Message) {})

	time.Sleep(50 * time.Millisecond) // let the background goroutine observe ctx.Err() and return
}

func TestListenerClose(t *testing.T) {
	consumer := kafka.NewConsumer(&kafka.Client{}, expectTracerStart(t))
	if err := consumer.Close(); err != nil {
		t.Errorf("Close() on a Client with no live connections = %v, want nil", err)
	}
}

func TestClientCloseJoinsBothErrors(t *testing.T) {
	c := &kafka.Client{
		Consumer: &fakeConsumerGroup{closeErr: errors.New("consumer boom")},
		Producer: &fakeSyncProducer{closeErr: errors.New("producer boom")},
	}

	err := c.Close()
	if err == nil {
		t.Fatal("Close() should return an error when both close calls fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "consumer boom") || !strings.Contains(msg, "producer boom") {
		t.Errorf("Close() error = %q, want it to mention both failures", msg)
	}
}
