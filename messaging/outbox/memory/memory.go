// Package memory is an in-memory outbox.Store adapter for tests and
// single-process development: it records every added event, makes an
// event visible to ClaimPending only once its transaction commits (via
// Tx, this package's outbox.TxHook-capable fake transaction), and applies
// the same claim/publish/retry/dead-letter fields outbox.Relay expects.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
)

// Store is an in-memory outbox.Store.
type Store struct {
	mu     sync.Mutex
	events map[string]outbox.Event
	order  []string // insertion order, for deterministic ClaimPending
}

// New returns an empty in-memory Store.
func New() *Store {
	return &Store{events: make(map[string]outbox.Event)}
}

var _ outbox.Store = (*Store)(nil)

// Add validates and records event. See outbox.Store.
//
// The tx must implement outbox.TxHook (this package's Tx does). A plain Tx
// without TxHook is treated as auto-commit so the contract degrades
// gracefully, matching scheduler/queue/memory's precedent.
func (s *Store) Add(_ context.Context, tx outbox.Tx, event outbox.Event) error {
	if tx == nil {
		return outbox.ErrNilTx
	}
	if err := event.Validate(); err != nil {
		return err
	}

	commit := func() { s.store(event) }
	hook, transactional := tx.(outbox.TxHook)
	if !transactional {
		commit()
		return nil
	}
	hook.OnCommit(commit)
	return nil
}

func (s *Store) store(event outbox.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.events[event.ID]; !exists {
		s.order = append(s.order, event.ID)
	}
	s.events[event.ID] = event
}

// ClaimPending returns up to limit non-dead events whose NextRetryAt has
// passed, in insertion order. A single in-process Store needs no locking
// beyond the mutex already held: there is only ever one claimant.
func (s *Store) ClaimPending(_ context.Context, limit int) ([]outbox.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	var claimed []outbox.Event
	for _, id := range s.order {
		if limit > 0 && len(claimed) >= limit {
			break
		}
		e, ok := s.events[id]
		if !ok || e.Dead {
			continue
		}
		if e.NextRetryAt.After(now) {
			continue
		}
		claimed = append(claimed, e)
	}
	return claimed, nil
}

// MarkPublished removes the events with the given ids. See outbox.Store.
func (s *Store) MarkPublished(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.events, id)
	}
	s.order = removeIDs(s.order, ids)
	return nil
}

// MarkFailed persists each event's updated fields. See outbox.Store.
func (s *Store) MarkFailed(_ context.Context, events []outbox.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range events {
		if _, ok := s.events[e.ID]; ok {
			s.events[e.ID] = e
		}
	}
	return nil
}

func removeIDs(order []string, ids []string) []string {
	toRemove := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		toRemove[id] = struct{}{}
	}
	out := order[:0:0]
	for _, id := range order {
		if _, drop := toRemove[id]; !drop {
			out = append(out, id)
		}
	}
	return out
}

// Snapshot returns a defensive copy of every event currently stored
// (pending or otherwise not yet published/removed), for assertions.
func (s *Store) Snapshot() []outbox.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]outbox.Event, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.events[id])
	}
	return out
}
