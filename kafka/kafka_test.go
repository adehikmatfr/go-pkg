package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

type fakeClaim struct {
	messages chan *sarama.ConsumerMessage
}

func (f *fakeClaim) Topic() string                            { return "test-topic" }
func (f *fakeClaim) Partition() int32                         { return 0 }
func (f *fakeClaim) InitialOffset() int64                     { return 0 }
func (f *fakeClaim) HighWaterMarkOffset() int64               { return 0 }
func (f *fakeClaim) Messages() <-chan *sarama.ConsumerMessage { return f.messages }

type fakeSession struct {
	marked int
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
	f.marked++
}
func (f *fakeSession) Context() context.Context { return context.Background() }

func TestConsumeClaimProcessesAllMessages(t *testing.T) {
	claim := &fakeClaim{messages: make(chan *sarama.ConsumerMessage, 2)}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte("1")}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte("2")}
	close(claim.messages)

	var handled int
	h := consumerGroupHandler{
		ctx: context.Background(),
		handle: func(ctx context.Context, msg *sarama.ConsumerMessage) {
			handled++
		},
	}

	session := &fakeSession{}
	if err := h.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim() error: %v", err)
	}
	if handled != 2 {
		t.Errorf("handled = %d, want 2", handled)
	}
	if session.marked != 2 {
		t.Errorf("marked = %d, want 2", session.marked)
	}
}

// TestConsumeClaimRecoversFromPanic ensures a panicking handler surfaces as
// an error from ConsumeClaim instead of crashing the consumer goroutine.
func TestConsumeClaimRecoversFromPanic(t *testing.T) {
	claim := &fakeClaim{messages: make(chan *sarama.ConsumerMessage, 1)}
	claim.messages <- &sarama.ConsumerMessage{Topic: "t", Value: []byte("boom")}
	close(claim.messages)

	h := consumerGroupHandler{
		ctx: context.Background(),
		handle: func(ctx context.Context, msg *sarama.ConsumerMessage) {
			panic("handler exploded")
		},
	}

	err := h.ConsumeClaim(&fakeSession{}, claim)
	if err == nil {
		t.Fatal("ConsumeClaim() should return an error when the handler panics")
	}
}

func TestListenToTopicStopsOnContextCancel(t *testing.T) {
	// A pre-canceled context must make the consume loop return via ctx.Err()
	// before it ever touches l.client.Consumer — regression test for a
	// previous version that busy-looped (and could nil-deref) once the
	// consumer was closed out from under it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	l := &Listener{client: &Client{}, tracer: noopTracer{}}
	l.ListenToTopic(ctx, "topic", func(context.Context, *sarama.ConsumerMessage) {})

	time.Sleep(50 * time.Millisecond) // let the background goroutine observe ctx.Err() and return
}
