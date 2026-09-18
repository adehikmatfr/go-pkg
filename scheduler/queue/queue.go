// Package queue is the durable background-job port: a vendor-free
// JobEnqueuer/HandlerRegistry consumers depend on, plus the Job domain type
// and the shared Tx seam every implementation uses. No third-party import —
// a concrete adapter (e.g. scheduler/queue/river) keeps its vendor types
// internal.
//
// A job's IdempotencyKey is the uniqueness key: a retried enqueue with the
// same key (and kind) creates no second job. Any job mutating state that
// must not be double-applied sets it.
package queue

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Sentinel errors, matched with errors.Is.
var (
	// ErrNilJob is returned when a nil JobArgs is enqueued.
	ErrNilJob = errors.New("queue: nil job")
	// ErrEmptyKind is returned when a job's Kind() is empty. Every job kind
	// must be a stable, non-empty identifier so workers can be matched
	// across deploys.
	ErrEmptyKind = errors.New("queue: empty job kind")
	// ErrNilTx is returned by EnqueueTx when the provided Tx is nil.
	ErrNilTx = errors.New("queue: nil transaction")
	// ErrAlreadyStarted is returned by Start when called more than once, or
	// by RegisterHandler after Start.
	ErrAlreadyStarted = errors.New("queue: already started")
)

// JobArgs is a single unit of background work. Implementations are plain,
// JSON-serializable structs: every exported field is persisted with the job
// and handed back to the matching handler when the job runs.
type JobArgs interface {
	// Kind is a stable, non-empty identifier for this job type, e.g.
	// "email_send". It routes a persisted job to its handler and must not
	// change once jobs of this kind exist in storage.
	Kind() string
}

// Job couples a set of JobArgs with per-enqueue options. Construct it with
// NewJob and refine it with the With* options.
type Job struct {
	// Args is the payload. Required.
	Args JobArgs

	// IdempotencyKey, when non-empty, makes the enqueue idempotent: a second
	// enqueue with the same key (and kind) does not create a second job.
	// Empty means "no uniqueness" — only safe for jobs that are naturally
	// safe to run more than once.
	IdempotencyKey string

	// Queue selects a named worker queue. Empty uses the adapter's default
	// queue.
	Queue string

	// MaxAttempts caps total attempts (original + retries). Zero uses the
	// adapter default.
	MaxAttempts int

	// ScheduledAt delays execution until at/after this time. Zero runs as
	// soon as a worker is free.
	ScheduledAt time.Time
}

// NewJob builds a Job from args plus options.
func NewJob(args JobArgs, opts ...JobOption) Job {
	j := Job{Args: args}
	for _, o := range opts {
		o(&j)
	}
	return j
}

// JobOption refines a Job built by NewJob.
type JobOption func(*Job)

// WithIdempotencyKey sets the job's idempotency key. See Job.IdempotencyKey.
func WithIdempotencyKey(key string) JobOption {
	return func(j *Job) { j.IdempotencyKey = key }
}

// WithQueue routes the job to a named queue.
func WithQueue(q string) JobOption {
	return func(j *Job) { j.Queue = q }
}

// WithMaxAttempts caps total attempts for the job.
func WithMaxAttempts(n int) JobOption {
	return func(j *Job) { j.MaxAttempts = n }
}

// WithScheduledAt delays the job until at/after t.
func WithScheduledAt(t time.Time) JobOption {
	return func(j *Job) { j.ScheduledAt = t }
}

// EnqueueResult reports the outcome of an enqueue.
type EnqueueResult struct {
	// ID is the adapter-assigned job identifier.
	ID int64
	// Kind echoes the job's kind.
	Kind string
	// AlreadyExisted is true when an idempotent enqueue matched an existing
	// job instead of creating a new one. Callers can use it to detect
	// retries.
	AlreadyExisted bool
}

// Result mirrors the subset of database/sql.Result the port relies on.
type Result interface {
	RowsAffected() (int64, error)
}

// Tx is the minimal transaction handle the EnqueueTx path needs. It is
// intentionally an interface (not *sql.Tx) so a non-SQL adapter, or a test
// fake, can implement it without this package importing anything a caller
// doesn't already have.
type Tx interface {
	// ExecContext executes a query within the transaction. A SQL-backed
	// adapter uses the wrapped *sql.Tx; the value passed must be the same
	// handle as the surrounding business write so the job is atomic with
	// it.
	ExecContext(ctx context.Context, query string, args ...any) (Result, error)
}

// TxHook is implemented by a Tx that can notify an adapter when the
// surrounding transaction commits or rolls back. A real *sql.Tx does not
// implement it — a SQL-backed adapter instead inserts directly into the
// transaction, where the database itself provides that atomicity; a
// non-transactional test fake (e.g. scheduler/queue/memory's Tx) implements
// it so the EnqueueTx contract is testable without a real database.
type TxHook interface {
	// OnCommit registers fn to run if/when the transaction commits.
	OnCommit(fn func())
	// OnRollback registers fn to run if/when the transaction rolls back.
	OnRollback(fn func())
}

// SQLTx adapts a *database/sql.Tx to the Tx port. Wrap the same *sql.Tx as
// the surrounding business writes so the job commits atomically with them.
//
//	tx, _ := db.BeginTx(ctx, nil)
//	_, _ = enqueuer.EnqueueTx(ctx, queue.NewSQLTx(tx), job)
//	// ... other writes on tx ...
//	tx.Commit() // job becomes durable here; rollback discards it
type SQLTx struct {
	tx *sql.Tx
}

// NewSQLTx wraps tx as a queue Tx.
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

// ValidateJob reports whether job is structurally sendable: Args must be
// non-nil with a non-empty Kind(). Every adapter calls this before touching
// its backend so the failure mode is identical across adapters.
func ValidateJob(job Job) error {
	if job.Args == nil {
		return ErrNilJob
	}
	if job.Args.Kind() == "" {
		return ErrEmptyKind
	}
	return nil
}

// JobEnqueuer is the port for enqueuing durable background jobs.
//
// Enqueue inserts outside any caller transaction. EnqueueTx inserts inside
// the provided transaction: the job is durable iff the surrounding
// transaction commits.
type JobEnqueuer interface {
	// Enqueue durably enqueues job. It returns ErrNilJob/ErrEmptyKind for an
	// invalid job, and wraps any adapter error with %w.
	Enqueue(ctx context.Context, job Job) (EnqueueResult, error)

	// EnqueueTx enqueues job within tx so it is committed atomically with
	// the caller's other writes. tx must be the same transaction as those
	// writes. Returns ErrNilTx for a nil transaction.
	EnqueueTx(ctx context.Context, tx Tx, job Job) (EnqueueResult, error)
}

// HandlerFunc processes one job. The raw, JSON-encoded args are passed so a
// handler can decode them into its own JobArgs type. Returning an error
// causes the adapter to retry per the job's MaxAttempts.
type HandlerFunc func(ctx context.Context, raw []byte) error

// HandlerRegistry registers a HandlerFunc for a job kind so the worker side
// can route persisted jobs to code. It is separate from JobEnqueuer because
// the enqueue side (request path) and the work side (worker process) often
// live in different binaries.
type HandlerRegistry interface {
	// RegisterHandler binds fn to kind. Registering the same kind twice
	// returns an error. kind must be non-empty.
	RegisterHandler(kind string, fn HandlerFunc) error
}
