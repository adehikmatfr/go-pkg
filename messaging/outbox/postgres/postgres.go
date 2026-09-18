// Package postgres is the production outbox.Store adapter over Postgres via
// github.com/lib/pq. It requires the following table to exist (the package
// does not run migrations — provision it with the caller's own migration
// tooling):
//
//	CREATE TABLE event_outbox (
//	    id              TEXT PRIMARY KEY,
//	    topic           TEXT NOT NULL,
//	    key             TEXT NOT NULL DEFAULT '',
//	    payload         BYTEA NOT NULL,
//	    retry_count     INTEGER NOT NULL DEFAULT 0,
//	    dead            BOOLEAN NOT NULL DEFAULT FALSE,
//	    created_at      TIMESTAMPTZ NOT NULL,
//	    next_retry_at   TIMESTAMPTZ NOT NULL,
//	    last_attempt_at TIMESTAMPTZ
//	);
//	CREATE INDEX ON event_outbox (next_retry_at) WHERE NOT dead;
//
// ClaimPending uses `UPDATE ... FOR UPDATE SKIP LOCKED ... RETURNING`,
// leasing each claimed row by pushing its next_retry_at forward by
// claimLease, so a concurrent Relay's ClaimPending never returns the same
// row while a claim is in flight.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
)

// claimLease is how far ClaimPending pushes next_retry_at forward on a
// claimed row, so it is not claimed again while this attempt is still in
// flight. It must comfortably exceed the time a single publish attempt can
// take.
const claimLease = 30 * time.Second

// DefaultTable is the table name used when Config.Table is empty.
const DefaultTable = "event_outbox"

// Config configures the Postgres-backed Store.
type Config struct {
	// DB is the application's database handle. Required.
	DB *sql.DB
	// Table overrides the outbox table name. Empty uses DefaultTable.
	Table string
}

type store struct {
	db    *sql.DB
	table string
}

var _ outbox.Store = (*store)(nil)

// New constructs a Postgres-backed outbox.Store. It performs no I/O
// (mirrors database/sql.Open), so it is testable without a live database.
// It returns an error if cfg.DB is nil.
func New(cfg Config) (outbox.Store, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("outbox/postgres: nil *sql.DB")
	}
	table := cfg.Table
	if table == "" {
		table = DefaultTable
	}
	return &store{db: cfg.DB, table: table}, nil
}

// sqlTxUnwrapper is implemented by an outbox.Tx that carries a *sql.Tx
// (e.g. outbox.SQLTx).
type sqlTxUnwrapper interface {
	Unwrap() *sql.Tx
}

func unwrapSQLTx(tx outbox.Tx) (*sql.Tx, error) {
	if tx == nil {
		return nil, outbox.ErrNilTx
	}
	u, ok := tx.(sqlTxUnwrapper)
	if !ok {
		return nil, fmt.Errorf("outbox/postgres: Add requires a *sql.Tx; wrap it with outbox.NewSQLTx")
	}
	sqlTx := u.Unwrap()
	if sqlTx == nil {
		return nil, outbox.ErrNilTx
	}
	return sqlTx, nil
}

// Add inserts event within tx. See outbox.Store. Requires a live *sql.Tx;
// the tx-unwrapping and event validation are covered without one.
func (s *store) Add(ctx context.Context, tx outbox.Tx, event outbox.Event) error {
	sqlTx, err := unwrapSQLTx(tx)
	if err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return s.addWithSQLTx(ctx, sqlTx, event)
}

func (s *store) addWithSQLTx(ctx context.Context, tx *sql.Tx, event outbox.Event) error { // coverage-ignore
	query := fmt.Sprintf(`
		INSERT INTO %s (id, topic, key, payload, retry_count, dead, created_at, next_retry_at, last_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, s.table)
	_, err := tx.ExecContext(ctx, query,
		event.ID, event.Topic, event.Key, event.Payload, event.RetryCount, event.Dead,
		event.CreatedAt, event.NextRetryAt, nullableTime(event.LastAttemptAt))
	if err != nil {
		return fmt.Errorf("outbox/postgres: insert: %w", err)
	}
	return nil
}

// ClaimPending leases up to limit due, non-dead rows via
// UPDATE...FOR UPDATE SKIP LOCKED...RETURNING, so concurrent relays never
// claim the same row. Requires a live database.
func (s *store) ClaimPending(ctx context.Context, limit int) ([]outbox.Event, error) { // coverage-ignore
	query := fmt.Sprintf(`
		UPDATE %[1]s
		SET next_retry_at = now() + $1::interval
		WHERE id IN (
			SELECT id FROM %[1]s
			WHERE NOT dead AND next_retry_at <= now()
			ORDER BY next_retry_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, topic, key, payload, retry_count, dead, created_at, next_retry_at, last_attempt_at`, s.table)

	rows, err := s.db.QueryContext(ctx, query, claimLease.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("outbox/postgres: claim pending: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []outbox.Event
	for rows.Next() {
		var e outbox.Event
		var lastAttempt sql.NullTime
		if err := rows.Scan(&e.ID, &e.Topic, &e.Key, &e.Payload, &e.RetryCount, &e.Dead,
			&e.CreatedAt, &e.NextRetryAt, &lastAttempt); err != nil {
			return nil, fmt.Errorf("outbox/postgres: scan claimed row: %w", err)
		}
		if lastAttempt.Valid {
			e.LastAttemptAt = lastAttempt.Time
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox/postgres: iterate claimed rows: %w", err)
	}
	return events, nil
}

// MarkPublished deletes the rows with the given ids. Requires a live
// database.
func (s *store) MarkPublished(ctx context.Context, ids []string) error { // coverage-ignore
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, s.table, strings.Join(placeholders, ", "))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("outbox/postgres: mark published: %w", err)
	}
	return nil
}

// MarkFailed persists each event's updated fields in one transaction, so a
// batch's reschedule/dead-letter transitions commit all-or-nothing.
// Requires a live database.
func (s *store) MarkFailed(ctx context.Context, events []outbox.Event) error { // coverage-ignore
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outbox/postgres: mark failed: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := fmt.Sprintf(`
		UPDATE %s
		SET retry_count = $1, dead = $2, next_retry_at = $3, last_attempt_at = $4
		WHERE id = $5`, s.table)
	for _, e := range events {
		if _, err := tx.ExecContext(ctx, query, e.RetryCount, e.Dead, e.NextRetryAt, nullableTime(e.LastAttemptAt), e.ID); err != nil {
			return fmt.Errorf("outbox/postgres: mark failed: update %q: %w", e.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("outbox/postgres: mark failed: commit: %w", err)
	}
	return nil
}

func nullableTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}
