// Package outbox is the transactional outbox port: a durable Store for
// events written atomically with a business change, plus a Relay that
// drains them through a messaging/broker.Publisher. It implements the
// outbox pattern: AddEvent's row commits (or rolls back) in the same
// transaction as the caller's business write, so "the business change
// happened" and "the event was recorded" can never disagree — Relay.Drain
// then delivers pending rows at-least-once, with retry and a dead-letter
// ceiling.
//
// No third-party import — a concrete adapter (e.g. messaging/outbox/postgres)
// keeps its vendor types internal.
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Sentinel errors, matched with errors.Is.
var (
	// ErrMissingID rejects an event without a stable id, which would make
	// MarkPublished/MarkFailed unable to target the right row.
	ErrMissingID = errors.New("outbox: event id is required")
	// ErrMissingTopic rejects an event without a destination topic.
	ErrMissingTopic = errors.New("outbox: event topic is required")
	// ErrNilTx is returned by Add when the provided Tx is nil.
	ErrNilTx = errors.New("outbox: nil transaction")
)

// Event is a single row in the outbox table.
type Event struct {
	// ID is a globally unique, stable identifier. Required.
	ID string
	// Topic is the destination topic passed to the publisher. Required.
	Topic string
	// Key is an optional ordering/dedup hint recorded alongside the event.
	// It is not transmitted to the publisher — messaging/broker.Publisher
	// carries no message-key concept, only topic and payload.
	Key string
	// Payload is the raw message body handed to the publisher unchanged.
	Payload []byte
	// RetryCount is the number of failed publish attempts so far.
	RetryCount int
	// Dead is true once RetryCount has reached the relay's attempt ceiling;
	// a dead event is never claimed again.
	Dead bool
	// CreatedAt is when the event was added.
	CreatedAt time.Time
	// NextRetryAt is the earliest time the event may be claimed again.
	NextRetryAt time.Time
	// LastAttemptAt is when the most recent publish attempt was made, zero
	// if none has happened yet.
	LastAttemptAt time.Time
}

// Validate reports whether the event carries the attributes a Store needs.
func (e Event) Validate() error {
	if e.ID == "" {
		return ErrMissingID
	}
	if e.Topic == "" {
		return ErrMissingTopic
	}
	return nil
}

// Result mirrors the subset of database/sql.Result the port relies on.
type Result interface {
	RowsAffected() (int64, error)
}

// Tx is the minimal transaction handle Store.Add needs. It is intentionally
// an interface (not *sql.Tx) so a non-SQL adapter, or a test fake, can
// implement it without this package importing anything a caller doesn't
// already have.
type Tx interface {
	// ExecContext executes a query within the transaction. A SQL-backed
	// adapter uses the wrapped *sql.Tx; the value passed must be the same
	// handle as the surrounding business write so the event is atomic with
	// it.
	ExecContext(ctx context.Context, query string, args ...any) (Result, error)
}

// TxHook is implemented by a Tx that can notify an adapter when the
// surrounding transaction commits or rolls back. A real *sql.Tx does not
// implement it — a SQL-backed adapter instead inserts directly into the
// transaction, where the database itself provides that atomicity; a
// non-transactional test fake (e.g. messaging/outbox/memory's Tx)
// implements it so the Add contract is testable without a real database.
type TxHook interface {
	// OnCommit registers fn to run if/when the transaction commits.
	OnCommit(fn func())
	// OnRollback registers fn to run if/when the transaction rolls back.
	OnRollback(fn func())
}

// SQLTx adapts a *database/sql.Tx to the Tx port. Wrap the same *sql.Tx as
// the surrounding business writes so the event commits atomically with
// them.
//
//	tx, _ := db.BeginTx(ctx, nil)
//	_, _ = store.Add(ctx, outbox.NewSQLTx(tx), event)
//	// ... other writes on tx ...
//	tx.Commit() // event becomes durable here; rollback discards it
type SQLTx struct {
	tx *sql.Tx
}

// NewSQLTx wraps tx as an outbox Tx.
func NewSQLTx(tx *sql.Tx) *SQLTx { return &SQLTx{tx: tx} }

var _ Tx = (*SQLTx)(nil)

// ExecContext runs the query on the underlying *sql.Tx. See Tx. Requires a
// live *sql.Tx (database/sql.DB.BeginTx dials); NewSQLTx/Unwrap's identity
// contract is covered without one.
func (s *SQLTx) ExecContext(ctx context.Context, query string, args ...any) (Result, error) { // coverage-ignore
	res, err := s.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Unwrap returns the underlying *sql.Tx.
func (s *SQLTx) Unwrap() *sql.Tx { return s.tx }

// Store is the port for the durable outbox table. Implementations are
// expected to make ClaimPending safe for multiple concurrent Relay
// instances (e.g. via a `SELECT ... FOR UPDATE SKIP LOCKED`-style claim).
type Store interface {
	// Add persists event within tx. Returns ErrNilTx for a nil transaction,
	// or an error wrapping Event.Validate's sentinel for an invalid event.
	Add(ctx context.Context, tx Tx, event Event) error

	// ClaimPending returns up to limit non-dead events whose NextRetryAt has
	// passed, claiming them so a concurrent ClaimPending does not return the
	// same row.
	ClaimPending(ctx context.Context, limit int) ([]Event, error)

	// MarkPublished removes (or otherwise finalizes) the events with the
	// given ids after a successful publish.
	MarkPublished(ctx context.Context, ids []string) error

	// MarkFailed persists each event's updated RetryCount/Dead/NextRetryAt/
	// LastAttemptAt after a failed publish attempt.
	MarkFailed(ctx context.Context, events []Event) error
}

// Clock returns the current time, so tests can drive retry/dead-letter
// scheduling deterministically. Package-local, matching the security/otp
// precedent, rather than a shared clock dependency for the few packages
// that actually need one.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// BackoffFunc computes the delay before an event may be claimed again,
// given the attempt number just made (1-based: the value RetryCount holds
// after incrementing).
type BackoffFunc func(attempt int) time.Duration

// DefaultBackoff doubles the delay per attempt starting at 1s, capped at 1h:
// 2s, 4s, 8s, ... A relay with the default ceiling (DefaultMaxAttempts)
// never waits past the cap before an event goes dead.
func DefaultBackoff(attempt int) time.Duration {
	d := time.Second * time.Duration(uint64(1)<<uint(attempt))
	const maxDelay = time.Hour
	if d > maxDelay || d <= 0 { // the shift overflows to <=0 for a large attempt
		d = maxDelay
	}
	return d
}
