package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/datastore/rdbms"
	rdbmspostgres "github.com/adehikmatfr/go-pkg/v2/datastore/rdbms/postgres"
	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox/memory"
	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox/postgres"
)

// newTestDB returns a *sql.DB that never dials — sql.Open only validates the
// DSN string, so this is safe to run without a live Postgres instance. See
// test/unit/datastore/rdbms/postgres for the same pattern.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := rdbmspostgres.New(&rdbms.Config{DSN: "postgres://user:pass@localhost:5432/db?sslmode=disable"})
	if err != nil {
		t.Fatalf("rdbmspostgres.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNew_NilDB(t *testing.T) {
	if _, err := postgres.New(postgres.Config{}); err == nil {
		t.Fatalf("expected an error for a nil *sql.DB")
	}
}

func TestNew_Valid(t *testing.T) {
	s, err := postgres.New(postgres.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatalf("expected a non-nil Store")
	}
}

func TestNew_DefaultTable(t *testing.T) {
	// Table defaulting has no externally observable seam beyond "New
	// succeeds with an empty Table", which is exercised by TestNew_Valid
	// (Config{}.Table is already empty there). This test documents the
	// contract explicitly.
	s, err := postgres.New(postgres.Config{DB: newTestDB(t), Table: ""})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatalf("expected a non-nil Store")
	}
}

func TestAdd_NilTx(t *testing.T) {
	s, err := postgres.New(postgres.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Add(context.Background(), nil, outbox.Event{ID: "evt-1", Topic: "orders"}); !errors.Is(err, outbox.ErrNilTx) {
		t.Fatalf("got err %v, want ErrNilTx", err)
	}
}

func TestAdd_RequiresSQLTx(t *testing.T) {
	s, err := postgres.New(postgres.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// messaging/outbox/memory's Tx is a valid outbox.Tx but is not backed by
	// a *sql.Tx, so this adapter cannot operate on it.
	err = s.Add(context.Background(), memory.NewTx(), outbox.Event{ID: "evt-1", Topic: "orders"})
	if err == nil {
		t.Fatalf("expected an error for a non-*sql.Tx-backed Tx")
	}
}

func TestAdd_NilUnderlyingSQLTx(t *testing.T) {
	s, err := postgres.New(postgres.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = s.Add(context.Background(), outbox.NewSQLTx(nil), outbox.Event{ID: "evt-1", Topic: "orders"})
	if !errors.Is(err, outbox.ErrNilTx) {
		t.Fatalf("got err %v, want ErrNilTx for a Tx wrapping a nil *sql.Tx", err)
	}
}
