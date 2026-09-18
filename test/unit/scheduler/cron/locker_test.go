package cron_test

import (
	"context"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
)

func TestNoopLocker(t *testing.T) {
	l := cron.NewNoopLocker()
	unlock, err := l.Lock(context.Background(), "any-key")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := unlock.Unlock(context.Background()); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// A second, concurrent-looking acquisition also always succeeds.
	if _, err := l.Lock(context.Background(), "any-key"); err != nil {
		t.Fatalf("second Lock: %v", err)
	}
}

func TestInMemoryLocker(t *testing.T) {
	l := cron.NewInMemoryLocker()

	unlock, err := l.Lock(context.Background(), "job-a")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if _, err := l.Lock(context.Background(), "job-a"); !errors.Is(err, cron.ErrLockHeld) {
		t.Fatalf("got err %v, want ErrLockHeld for an already-held key", err)
	}

	// A different key is unaffected.
	unlockB, err := l.Lock(context.Background(), "job-b")
	if err != nil {
		t.Fatalf("Lock(job-b): %v", err)
	}
	if err := unlockB.Unlock(context.Background()); err != nil {
		t.Fatalf("Unlock(job-b): %v", err)
	}

	if err := unlock.Unlock(context.Background()); err != nil {
		t.Fatalf("Unlock(job-a): %v", err)
	}

	// Once released, the key is acquirable again.
	unlock2, err := l.Lock(context.Background(), "job-a")
	if err != nil {
		t.Fatalf("re-Lock(job-a) after release: %v", err)
	}

	// Unlock is idempotent (safe to call more than once).
	if err := unlock2.Unlock(context.Background()); err != nil {
		t.Fatalf("first Unlock: %v", err)
	}
	if err := unlock2.Unlock(context.Background()); err != nil {
		t.Fatalf("second Unlock (idempotent): %v", err)
	}
}
