package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/audit"
	"github.com/adehikmatfr/go-pkg/v2/audit/memory"
)

func event(id string) audit.AuditEvent {
	return audit.AuditEvent{
		ID:     id,
		Source: "/orders",
		Type:   "tech.example.order.placed",
		Time:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestStore_Append(t *testing.T) {
	s := memory.New()

	hash1, err := s.Append(context.Background(), audit.GenesisHash, event("evt-1"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if hash1 == "" {
		t.Fatalf("expected a non-empty row hash")
	}
	if s.Tail() != hash1 {
		t.Fatalf("Tail() = %q, want %q", s.Tail(), hash1)
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}

	hash2, err := s.Append(context.Background(), hash1, event("evt-2"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if s.Len() != 2 || s.Tail() != hash2 {
		t.Fatalf("Len() = %d, Tail() = %q after second append", s.Len(), s.Tail())
	}
}

func TestStore_Append_InvalidEvent(t *testing.T) {
	s := memory.New()
	_, err := s.Append(context.Background(), audit.GenesisHash, audit.AuditEvent{})
	if !errors.Is(err, audit.ErrMissingID) {
		t.Fatalf("got err %v, want ErrMissingID", err)
	}
}

func TestStore_Append_CancelledContext(t *testing.T) {
	s := memory.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Append(ctx, audit.GenesisHash, event("evt-1")); err == nil {
		t.Fatalf("expected an error for a cancelled context")
	}
}

func TestStore_Append_RejectsForkedChain(t *testing.T) {
	s := memory.New()
	if _, err := s.Append(context.Background(), audit.GenesisHash, event("evt-1")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// A second append with a stale prevHash (not the current tail) forks the chain.
	if _, err := s.Append(context.Background(), audit.GenesisHash, event("evt-2")); !errors.Is(err, audit.ErrChainBroken) {
		t.Fatalf("got err %v, want ErrChainBroken", err)
	}
}

func TestStore_Append_Idempotent(t *testing.T) {
	s := memory.New()
	e := event("evt-1")

	hash1, err := s.Append(context.Background(), audit.GenesisHash, e)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	hash2, err := s.Append(context.Background(), audit.GenesisHash, e)
	if err != nil {
		t.Fatalf("re-Append of the same event: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("re-appending the same event id returned a different hash: %q vs %q", hash1, hash2)
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (re-append must not create a second link)", s.Len())
	}
}

func TestStore_Append_IdempotentConflictingPrevHashRejected(t *testing.T) {
	s := memory.New()
	e := event("evt-1")
	if _, err := s.Append(context.Background(), audit.GenesisHash, e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := s.Append(context.Background(), "some-other-hash", e); !errors.Is(err, audit.ErrChainBroken) {
		t.Fatalf("got err %v, want ErrChainBroken for a conflicting re-append", err)
	}
}

func TestStore_VerifyChain(t *testing.T) {
	s := memory.New()
	hash1, err := s.Append(context.Background(), audit.GenesisHash, event("evt-1"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := s.Append(context.Background(), hash1, event("evt-2")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := s.VerifyChain(context.Background(), nil); err != nil {
		t.Fatalf("VerifyChain(nil) on the store's own chain: %v", err)
	}

	tampered := s.Snapshot()
	tampered[0].Event.Subject = "tampered"
	if err := s.VerifyChain(context.Background(), tampered); !errors.Is(err, audit.ErrChainBroken) {
		t.Fatalf("got err %v, want ErrChainBroken for a tampered snapshot", err)
	}
}

func TestStore_VerifyChain_CancelledContext(t *testing.T) {
	s := memory.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.VerifyChain(ctx, nil); err == nil {
		t.Fatalf("expected an error for a cancelled context")
	}
}

func TestStore_Snapshot_IsADefensiveCopy(t *testing.T) {
	s := memory.New()
	if _, err := s.Append(context.Background(), audit.GenesisHash, event("evt-1")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	snap := s.Snapshot()
	snap[0].Event.Subject = "mutated"

	if s.Snapshot()[0].Event.Subject == "mutated" {
		t.Fatalf("mutating a Snapshot() result affected the store's internal state")
	}
}

func TestStore_ConcurrentAppend(t *testing.T) {
	s := memory.New()
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			// Each goroutine races to append after whatever the current tail
			// is; only one prevHash per tail will succeed, and the rest are
			// expected to lose the race with ErrChainBroken.
			_, _ = s.Append(context.Background(), s.Tail(), event(idFor(i)))
		}(i)
	}
	wg.Wait()

	if err := s.VerifyChain(context.Background(), nil); err != nil {
		t.Fatalf("chain is inconsistent after concurrent appends: %v", err)
	}
}

func idFor(i int) string {
	return "evt-concurrent-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}
