package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/notification/push"
	"github.com/adehikmatfr/go-pkg/v2/notification/push/memory"
)

func TestSender_Send_returnsGeneratedID(t *testing.T) {
	s := memory.New()
	id, err := s.Send(context.Background(), push.PushMessage{Token: "tok", Title: "hi"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id == "" {
		t.Error("Send() returned an empty provider message ID")
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
}

func TestSender_Send_idFunc(t *testing.T) {
	s := memory.New()
	s.IDFunc = func(push.PushMessage) string { return "msg-123" }

	id, err := s.Send(context.Background(), push.PushMessage{Token: "tok", Title: "hi"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "msg-123" {
		t.Errorf("Send() id = %q, want msg-123", id)
	}
}

func TestSender_Send_validationShortCircuits(t *testing.T) {
	s := memory.New()
	_, err := s.Send(context.Background(), push.PushMessage{Title: "no recipient"})
	if !errors.Is(err, push.ErrNoRecipient) {
		t.Fatalf("Send() error = %v, want ErrNoRecipient", err)
	}
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0: a validation failure must not be recorded", s.Len())
	}
}

func TestSender_Send_errStillRecordsAttempt(t *testing.T) {
	injected := errors.New("provider down")
	s := memory.New()
	s.Err = injected

	_, err := s.Send(context.Background(), push.PushMessage{Token: "tok", Title: "hi"})
	if !errors.Is(err, injected) {
		t.Fatalf("Send() error = %v, want %v", err, injected)
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1: the attempt should still be recorded before failing", s.Len())
	}
}

func TestSender_Send_contextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := memory.New()
	_, err := s.Send(ctx, push.PushMessage{Token: "tok", Title: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", s.Len())
	}
}

func TestSender_Sent_recordsInOrderAndReturnsCopy(t *testing.T) {
	s := memory.New()
	if _, err := s.Send(context.Background(), push.PushMessage{Token: "a", Title: "1"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if _, err := s.Send(context.Background(), push.PushMessage{Token: "b", Title: "2"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	sent := s.Sent()
	if len(sent) != 2 || sent[0].Token != "a" || sent[1].Token != "b" {
		t.Fatalf("Sent() = %+v, want [a b] in order", sent)
	}

	sent[0].Token = "mutated"
	if got := s.Sent()[0].Token; got != "a" {
		t.Fatalf("internal state mutated via Sent() copy: got %q", got)
	}
}

func TestSender_Reset(t *testing.T) {
	s := memory.New()
	if _, err := s.Send(context.Background(), push.PushMessage{Token: "a", Title: "1"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	s.Reset()
	if s.Len() != 0 {
		t.Fatalf("Len() = %d after Reset(), want 0", s.Len())
	}
}
