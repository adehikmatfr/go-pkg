// Package memory is an in-memory audit.AuditStore adapter for tests and
// ephemeral use — a serialized chain tail, idempotent on event ID, so code
// can be developed without a database. A persistent adapter (e.g. backed by
// datastore/rdbms/postgres) is a plausible future addition with the same
// append-only, fork-resistant contract.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/adehikmatfr/go-pkg/v2/audit"
)

// Store is a concurrency-safe, in-memory audit.AuditStore.
type Store struct {
	mu      sync.RWMutex
	records []audit.Record
	// byID indexes records by event ID for O(1) idempotent appends.
	byID map[string]int
}

var _ audit.AuditStore = (*Store)(nil)

// New returns an empty in-memory audit.AuditStore.
func New() *Store {
	return &Store{byID: make(map[string]int)}
}

// Append validates the event and links it after prevHash. It enforces that
// prevHash matches the current chain tail so writers cannot fork the chain,
// and is idempotent on the event ID. See audit.AuditStore for the full
// contract.
func (s *Store) Append(ctx context.Context, prevHash string, event audit.AuditEvent) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := event.Validate(); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Idempotency: an already-recorded event returns its existing hash, and
	// a retry whose prevHash contradicts the recorded link is rejected, not
	// accepted.
	if idx, ok := s.byID[event.ID]; ok {
		existing := s.records[idx]
		if existing.PrevHash != prevHash {
			return "", fmt.Errorf("%w: duplicate event %q with conflicting prev hash",
				audit.ErrChainBroken, event.ID)
		}
		return existing.RowHash, nil
	}

	// Enforce single, well-ordered chain tail.
	tail := audit.GenesisHash
	if n := len(s.records); n > 0 {
		tail = s.records[n-1].RowHash
	}
	if prevHash != tail {
		return "", fmt.Errorf("%w: append prev hash %q does not match chain tail %q",
			audit.ErrChainBroken, prevHash, tail)
	}

	rowHash, err := audit.ComputeRowHash(prevHash, event)
	if err != nil {
		return "", err
	}

	s.records = append(s.records, audit.Record{
		PrevHash: prevHash,
		RowHash:  rowHash,
		Event:    event,
	})
	s.byID[event.ID] = len(s.records) - 1
	return rowHash, nil
}

// VerifyChain checks the supplied records against audit.VerifyChain's rules.
// Passing nil verifies the store's own current chain snapshot instead,
// which is convenient for self-auditing.
func (s *Store) VerifyChain(ctx context.Context, records []audit.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if records == nil {
		records = s.Snapshot()
	}
	return audit.VerifyChain(records)
}

// Snapshot returns a defensive copy of the store's current chain in order.
func (s *Store) Snapshot() []audit.Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]audit.Record, len(s.records))
	copy(out, s.records)
	return out
}

// Tail returns the current chain tail hash (audit.GenesisHash for an empty
// chain), which is the prevHash a caller should pass to the next Append.
func (s *Store) Tail() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n := len(s.records); n > 0 {
		return s.records[n-1].RowHash
	}
	return audit.GenesisHash
}

// Len returns the number of links currently in the chain.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}
