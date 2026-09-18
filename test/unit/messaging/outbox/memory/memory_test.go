package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox/memory"
)

func event(id string) outbox.Event {
	return outbox.Event{ID: id, Topic: "orders", CreatedAt: time.Now().UTC()}
}

func TestStore_Add_NilTx(t *testing.T) {
	s := memory.New()
	if err := s.Add(context.Background(), nil, event("evt-1")); !errors.Is(err, outbox.ErrNilTx) {
		t.Fatalf("got err %v, want ErrNilTx", err)
	}
}

func TestStore_Add_InvalidEvent(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()
	if err := s.Add(context.Background(), tx, outbox.Event{}); !errors.Is(err, outbox.ErrMissingID) {
		t.Fatalf("got err %v, want ErrMissingID", err)
	}
}

func TestStore_Add_VisibleOnCommit(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()

	if err := s.Add(context.Background(), tx, event("evt-1")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	claimed, err := s.ClaimPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("ClaimPending before commit = %d, want 0", len(claimed))
	}

	tx.Commit()

	claimed, err = s.ClaimPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "evt-1" {
		t.Fatalf("ClaimPending after commit = %+v, want [evt-1]", claimed)
	}
}

func TestStore_Add_DroppedOnRollback(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()

	if err := s.Add(context.Background(), tx, event("evt-1")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tx.Rollback()

	if len(s.Snapshot()) != 0 {
		t.Fatalf("Snapshot() after rollback = %d, want 0", len(s.Snapshot()))
	}
}

func TestStore_Add_NonTxHookTreatedAsAutoCommit(t *testing.T) {
	s := memory.New()
	tx := outbox.NewSQLTx(nil) // satisfies outbox.Tx, not outbox.TxHook

	if err := s.Add(context.Background(), tx, event("evt-1")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	claimed, err := s.ClaimPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected the event to be immediately visible (auto-commit), got %d", len(claimed))
	}
}

func TestStore_ClaimPending_RespectsNextRetryAtAndDead(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()

	future := event("evt-future")
	future.NextRetryAt = time.Now().Add(time.Hour)
	dead := event("evt-dead")
	dead.Dead = true
	due := event("evt-due")

	for _, e := range []outbox.Event{future, dead, due} {
		if err := s.Add(context.Background(), tx, e); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	tx.Commit()

	claimed, err := s.ClaimPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "evt-due" {
		t.Fatalf("ClaimPending() = %+v, want only evt-due", claimed)
	}
}

func TestStore_ClaimPending_RespectsLimit(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Add(context.Background(), tx, event(id)); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	tx.Commit()

	claimed, err := s.ClaimPending(context.Background(), 2)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("ClaimPending(limit=2) returned %d events, want 2", len(claimed))
	}
}

func TestStore_MarkPublished(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()
	if err := s.Add(context.Background(), tx, event("evt-1")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tx.Commit()

	if err := s.MarkPublished(context.Background(), []string{"evt-1"}); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}
	if len(s.Snapshot()) != 0 {
		t.Fatalf("expected the event to be removed after MarkPublished")
	}
}

func TestStore_MarkFailed(t *testing.T) {
	s := memory.New()
	tx := memory.NewTx()
	if err := s.Add(context.Background(), tx, event("evt-1")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tx.Commit()

	updated := event("evt-1")
	updated.RetryCount = 1
	updated.Dead = true
	if err := s.MarkFailed(context.Background(), []outbox.Event{updated}); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	snap := s.Snapshot()
	if len(snap) != 1 || !snap[0].Dead || snap[0].RetryCount != 1 {
		t.Fatalf("Snapshot() = %+v, want the updated event", snap)
	}
}
