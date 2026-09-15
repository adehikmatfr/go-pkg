package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
	"github.com/adehikmatfr/go-pkg/v2/notification/email/memory"
)

func validMessage(idempotencyKey string) email.EmailMessage {
	return email.EmailMessage{
		To:             []email.Address{{Email: "ada@example.com"}},
		TextBody:       "hello",
		IdempotencyKey: idempotencyKey,
	}
}

func TestSender_Send_recordsInOrder(t *testing.T) {
	s := memory.New()
	ctx := context.Background()

	if err := s.Send(ctx, validMessage("evt-1")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := s.Send(ctx, validMessage("evt-2")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if got := s.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}
	sent := s.Sent()
	if len(sent) != 2 || sent[0].IdempotencyKey != "evt-1" || sent[1].IdempotencyKey != "evt-2" {
		t.Fatalf("Sent() = %+v, want [evt-1 evt-2] in order", sent)
	}
	last, ok := s.Last()
	if !ok || last.IdempotencyKey != "evt-2" {
		t.Fatalf("Last() = %+v, %v, want evt-2, true", last, ok)
	}
}

func TestSender_Sent_returnsCopy(t *testing.T) {
	s := memory.New()
	if err := s.Send(context.Background(), validMessage("evt-1")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	sent := s.Sent()
	sent[0].IdempotencyKey = "mutated"

	if got, _ := s.Last(); got.IdempotencyKey != "evt-1" {
		t.Fatalf("internal state mutated via Sent() copy: got %q", got.IdempotencyKey)
	}
}

func TestSender_Last_emptyWhenNothingSent(t *testing.T) {
	s := memory.New()
	_, ok := s.Last()
	if ok {
		t.Fatalf("Last() ok = true on empty sender, want false")
	}
}

func TestSender_Send_validatesBeforeRecording(t *testing.T) {
	s := memory.New()
	invalid := email.EmailMessage{} // no recipients, no idempotency key, no body

	err := s.Send(context.Background(), invalid)
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("Send() error = %v, want ErrInvalidMessage", err)
	}
	if s.Count() != 0 {
		t.Fatalf("Count() = %d, want 0 after a validation failure", s.Count())
	}
}

func TestSender_Send_failWithInjection(t *testing.T) {
	injected := errors.New("smtp down")
	s := memory.New()
	s.FailWith = injected

	err := s.Send(context.Background(), validMessage("evt-1"))
	if !errors.Is(err, injected) {
		t.Fatalf("Send() error = %v, want %v", err, injected)
	}
	if s.Count() != 0 {
		t.Fatalf("Count() = %d, want 0 when FailWith is set", s.Count())
	}
}

func TestSender_Send_contextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := memory.New()
	err := s.Send(ctx, validMessage("evt-1"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
}

func TestSender_Reset(t *testing.T) {
	s := memory.New()
	if err := s.Send(context.Background(), validMessage("evt-1")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	s.Reset()
	if s.Count() != 0 {
		t.Fatalf("Count() = %d after Reset(), want 0", s.Count())
	}
}

func TestSender_Send_concurrentUse(t *testing.T) {
	s := memory.New()
	const n = 50
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			_ = s.Send(context.Background(), validMessage("evt"))
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for concurrent sends")
		}
	}
	if s.Count() != n {
		t.Fatalf("Count() = %d, want %d", s.Count(), n)
	}
}
