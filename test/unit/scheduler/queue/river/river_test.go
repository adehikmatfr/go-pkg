package river_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/datastore/rdbms"
	"github.com/adehikmatfr/go-pkg/v2/datastore/rdbms/postgres"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue/memory"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue/river"
)

var pastTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

type emailArgs struct {
	Kind_ string `json:"-"`
	To    string `json:"to"`
}

func (a emailArgs) Kind() string { return a.Kind_ }

func job(to string) queue.Job {
	return queue.NewJob(emailArgs{Kind_: "email_send", To: to})
}

// unmarshalableArgs is a JobArgs whose payload cannot be JSON-marshaled
// (func values are never marshalable), to exercise buildInsertOpts's
// marshal-error path.
type unmarshalableArgs struct{}

func (unmarshalableArgs) Kind() string { return "unmarshalable" }
func (unmarshalableArgs) MarshalJSON() ([]byte, error) {
	return nil, errMarshalUnsupported
}

var errMarshalUnsupported = errors.New("unsupported for test")

// newTestDB returns a *sql.DB that never dials — sql.Open only validates the
// DSN string, so this is safe to run without a live Postgres instance. See
// test/unit/datastore/rdbms/postgres for the same pattern.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := postgres.New(&rdbms.Config{DSN: "postgres://user:pass@localhost:5432/db?sslmode=disable"})
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNew_NilDB(t *testing.T) {
	if _, err := river.New(river.Config{}); err == nil {
		t.Fatalf("expected an error for a nil *sql.DB")
	}
}

func TestNew_Valid(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e == nil {
		t.Fatalf("expected a non-nil Enqueuer")
	}
}

func TestNew_WithQueues(t *testing.T) {
	e, err := river.New(river.Config{
		DB:                 newTestDB(t),
		Queues:             map[string]int{"default": 5},
		DefaultMaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e == nil {
		t.Fatalf("expected a non-nil Enqueuer")
	}
}

func TestEnqueue_InvalidJobNeverTouchesTheDatabase(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Validation happens before any client call, so this must fail fast
	// with ErrNilJob rather than attempting to reach a (non-existent) live
	// database.
	if _, err := e.Enqueue(context.Background(), queue.Job{}); !errors.Is(err, queue.ErrNilJob) {
		t.Fatalf("got err %v, want ErrNilJob", err)
	}
}

func TestEnqueueTx_NilTx(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := e.EnqueueTx(context.Background(), nil, job("a@example.com")); !errors.Is(err, queue.ErrNilTx) {
		t.Fatalf("got err %v, want ErrNilTx", err)
	}
}

func TestEnqueueTx_RequiresSQLTx(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// scheduler/queue/memory's Tx is a valid queue.Tx but is not backed by a
	// *sql.Tx, so this adapter cannot operate on it.
	_, err = e.EnqueueTx(context.Background(), memory.NewTx(), job("a@example.com"))
	if err == nil {
		t.Fatalf("expected an error for a non-*sql.Tx-backed Tx")
	}
}

func TestEnqueueTx_NilUnderlyingSQLTx(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = e.EnqueueTx(context.Background(), queue.NewSQLTx(nil), job("a@example.com"))
	if !errors.Is(err, queue.ErrNilTx) {
		t.Fatalf("got err %v, want ErrNilTx for a Tx wrapping a nil *sql.Tx", err)
	}
}

func TestEnqueue_OptionMapping(t *testing.T) {
	// The actual insert needs a live database, so every case here is
	// expected to fail past validation — the point is exercising
	// buildInsertOpts's pure Job -> river.InsertOpts mapping (queue, max
	// attempts, scheduled-at, idempotency) before that failure, not the
	// failure itself.
	tests := []struct {
		name string
		job  queue.Job
		cfg  river.Config
	}{
		{name: "explicit queue", job: queue.NewJob(emailArgs{Kind_: "email_send"}, queue.WithQueue("high"))},
		{name: "explicit max attempts", job: queue.NewJob(emailArgs{Kind_: "email_send"}, queue.WithMaxAttempts(7))},
		{name: "default max attempts from config", job: queue.NewJob(emailArgs{Kind_: "email_send"})},
		{name: "scheduled at", job: queue.NewJob(emailArgs{Kind_: "email_send"}, queue.WithScheduledAt(pastTime))},
		{name: "idempotency key", job: queue.NewJob(emailArgs{Kind_: "email_send"}, queue.WithIdempotencyKey("key-1"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.DB = newTestDB(t)
			if tt.name == "default max attempts from config" {
				cfg.DefaultMaxAttempts = 3
			}
			e, err := river.New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = e.Enqueue(context.Background(), tt.job)
			if errors.Is(err, queue.ErrNilJob) || errors.Is(err, queue.ErrEmptyKind) {
				t.Fatalf("got a validation error %v, want the job to pass buildInsertOpts and fail on the (absent) database instead", err)
			}
		})
	}
}

func TestEnqueue_MarshalError(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = e.Enqueue(context.Background(), queue.NewJob(unmarshalableArgs{}))
	if err == nil {
		t.Fatalf("expected an error for args that cannot be JSON-marshaled")
	}
}

func TestRegisterHandler(t *testing.T) {
	e, err := river.New(river.Config{DB: newTestDB(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.RegisterHandler("", func(context.Context, []byte) error { return nil }); !errors.Is(err, queue.ErrEmptyKind) {
		t.Fatalf("got err %v, want ErrEmptyKind", err)
	}
	if err := e.RegisterHandler("email_send", nil); err == nil {
		t.Fatalf("expected an error for a nil handler func")
	}
	if err := e.RegisterHandler("email_send", func(context.Context, []byte) error { return nil }); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if err := e.RegisterHandler("email_send", func(context.Context, []byte) error { return nil }); err == nil {
		t.Fatalf("expected an error for a duplicate handler registration")
	}
}
