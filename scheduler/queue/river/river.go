// Package river is the production queue.JobEnqueuer and
// queue.HandlerRegistry adapter backed by riverqueue/river over
// database/sql. River types stay internal to this package; consumers see
// only the queue port and domain structs.
//
// EnqueueTx inserts the job row in the caller's *sql.Tx (wrapped via
// queue.NewSQLTx), so a job commits only if the surrounding business
// transaction does. Idempotency comes from river's unique-job machinery — a
// database unique constraint keyed off the job's IdempotencyKey — so a
// redelivered enqueue cannot create a duplicate.
package river

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	vendorriver "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
)

// Enqueuer is the production queue.JobEnqueuer and queue.HandlerRegistry
// backed by riverqueue/river.
type Enqueuer struct {
	client *vendorriver.Client[*sql.Tx]

	mu        sync.Mutex
	workers   *vendorriver.Workers
	handlers  map[string]queue.HandlerFunc
	started   bool
	defaultMA int
}

// Config configures the river-backed enqueuer.
type Config struct {
	// DB is the application's database handle. Required.
	DB *sql.DB

	// Queues maps queue name to its max concurrent workers. Workers are
	// only started for listed queues. If empty, only enqueueing is
	// available (no in-process worker) — the common split where a separate
	// worker binary runs the jobs.
	Queues map[string]int

	// DefaultMaxAttempts is applied to jobs that don't set MaxAttempts.
	// Zero uses river's default.
	DefaultMaxAttempts int
}

var (
	_ queue.JobEnqueuer     = (*Enqueuer)(nil)
	_ queue.HandlerRegistry = (*Enqueuer)(nil)
)

// New constructs a river-backed Enqueuer. It builds the river client
// immediately so Enqueue works; call Start to also process jobs in this
// process. It returns an error if cfg.DB is nil or the client cannot be
// built. Building the client performs no I/O (mirrors database/sql.Open),
// so this is testable without a live database.
func New(cfg Config) (*Enqueuer, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("queue/river: nil *sql.DB")
	}
	workers := vendorriver.NewWorkers()

	riverCfg := &vendorriver.Config{}
	if len(cfg.Queues) > 0 {
		riverCfg.Workers = workers
		riverCfg.Queues = make(map[string]vendorriver.QueueConfig, len(cfg.Queues))
		for name, maxWorkers := range cfg.Queues {
			riverCfg.Queues[name] = vendorriver.QueueConfig{MaxWorkers: maxWorkers}
		}
	}

	client, err := vendorriver.NewClient(riverdatabasesql.New(cfg.DB), riverCfg)
	if err != nil {
		return nil, fmt.Errorf("queue/river: build client: %w", err)
	}
	return &Enqueuer{
		client:    client,
		workers:   workers,
		handlers:  make(map[string]queue.HandlerFunc),
		defaultMA: cfg.DefaultMaxAttempts,
	}, nil
}

// genericArgs is the internal river job-args envelope carrying the
// consumer's raw JSON payload and the idempotency key. Only IdempotencyKey
// is tagged river:"unique", so the uniqueness hash depends on (kind,
// idempotency key) and never on the payload. The handler unwraps Payload
// before invoking the consumer HandlerFunc.
type genericArgs struct {
	kind           string
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key,omitempty" river:"unique"`
}

// Kind satisfies river.JobArgs.
func (g genericArgs) Kind() string { return g.kind }

// buildInsertOpts translates a queue.Job into river insert options. It is
// pure, so the idempotency, queue and attempts mapping is testable without
// a database.
func (r *Enqueuer) buildInsertOpts(job queue.Job) (genericArgs, *vendorriver.InsertOpts, error) {
	if err := queue.ValidateJob(job); err != nil {
		return genericArgs{}, nil, err
	}
	payload, err := json.Marshal(job.Args)
	if err != nil {
		return genericArgs{}, nil, fmt.Errorf("queue/river: marshal job %q: %w", job.Args.Kind(), err)
	}
	args := genericArgs{
		kind:           job.Args.Kind(),
		Payload:        payload,
		IdempotencyKey: job.IdempotencyKey,
	}

	opts := &vendorriver.InsertOpts{}
	if job.Queue != "" {
		opts.Queue = job.Queue
	}
	switch {
	case job.MaxAttempts > 0:
		opts.MaxAttempts = job.MaxAttempts
	case r.defaultMA > 0:
		opts.MaxAttempts = r.defaultMA
	}
	if !job.ScheduledAt.IsZero() {
		opts.ScheduledAt = job.ScheduledAt
	}
	if job.IdempotencyKey != "" {
		// ByArgs hashes only fields tagged river:"unique" (IdempotencyKey),
		// giving (kind, key) uniqueness backed by a DB unique constraint.
		opts.UniqueOpts = vendorriver.UniqueOpts{ByArgs: true}
	}
	return args, opts, nil
}

// Enqueue inserts the job outside any caller transaction. See
// queue.JobEnqueuer. The actual insert requires a live database (river's
// schema must be migrated); the pure request-shaping logic is covered via
// buildInsertOpts.
func (r *Enqueuer) Enqueue(ctx context.Context, job queue.Job) (queue.EnqueueResult, error) {
	args, opts, err := r.buildInsertOpts(job)
	if err != nil {
		return queue.EnqueueResult{}, err
	}
	res, err := r.client.Insert(ctx, args, opts)
	if err != nil {
		return queue.EnqueueResult{}, fmt.Errorf("queue/river: insert %q: %w", job.Args.Kind(), err)
	}
	return queue.EnqueueResult{
		ID:             res.Job.ID,
		Kind:           job.Args.Kind(),
		AlreadyExisted: res.UniqueSkippedAsDuplicate,
	}, nil
}

// EnqueueTx inserts the job inside the caller's transaction so it is atomic
// with the caller's other writes. tx must carry a *sql.Tx — pass one via
// queue.NewSQLTx, the same *sql.Tx as the surrounding business write. See
// queue.JobEnqueuer. The tx-unwrapping validation is covered without a
// database; the actual insert requires a live one.
func (r *Enqueuer) EnqueueTx(ctx context.Context, tx queue.Tx, job queue.Job) (queue.EnqueueResult, error) {
	if tx == nil {
		return queue.EnqueueResult{}, queue.ErrNilTx
	}
	sqlTx, err := unwrapSQLTx(tx)
	if err != nil {
		return queue.EnqueueResult{}, err
	}
	args, opts, err := r.buildInsertOpts(job)
	if err != nil {
		return queue.EnqueueResult{}, err
	}
	res, err := r.client.InsertTx(ctx, sqlTx, args, opts)
	if err != nil {
		return queue.EnqueueResult{}, fmt.Errorf("queue/river: insert tx %q: %w", job.Args.Kind(), err)
	}
	return queue.EnqueueResult{
		ID:             res.Job.ID,
		Kind:           job.Args.Kind(),
		AlreadyExisted: res.UniqueSkippedAsDuplicate,
	}, nil
}

// sqlTxUnwrapper is implemented by a queue.Tx that carries a *sql.Tx (e.g.
// queue.SQLTx).
type sqlTxUnwrapper interface {
	Unwrap() *sql.Tx
}

// unwrapSQLTx extracts the *sql.Tx the river driver needs from a queue.Tx.
// It returns a descriptive error when the Tx is not backed by a *sql.Tx
// (e.g. scheduler/queue/memory's Tx), since this adapter cannot operate on
// a non-SQL transaction.
func unwrapSQLTx(tx queue.Tx) (*sql.Tx, error) {
	u, ok := tx.(sqlTxUnwrapper)
	if !ok {
		return nil, fmt.Errorf("queue/river: EnqueueTx requires a *sql.Tx; wrap it with queue.NewSQLTx")
	}
	sqlTx := u.Unwrap()
	if sqlTx == nil {
		return nil, queue.ErrNilTx
	}
	return sqlTx, nil
}

// RegisterHandler binds a HandlerFunc to a kind and registers an internal
// river worker that decodes the envelope and dispatches to it. Call before
// Start. See queue.HandlerRegistry.
func (r *Enqueuer) RegisterHandler(kind string, fn queue.HandlerFunc) error {
	if kind == "" {
		return queue.ErrEmptyKind
	}
	if fn == nil {
		return fmt.Errorf("queue/river: nil handler func")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return queue.ErrAlreadyStarted
	}
	if _, dup := r.handlers[kind]; dup {
		return fmt.Errorf("queue/river: handler already registered for kind %q", kind)
	}
	r.handlers[kind] = fn
	// Register the worker under this specific kind. All kinds share the
	// genericArgs Go type, so we must pass an explicitly-kinded args
	// instance (AddWorkerArgs) rather than letting river derive the kind
	// from a zero-value (which would be empty and collide across kinds).
	vendorriver.AddWorkerArgs[genericArgs](r.workers, genericArgs{kind: kind}, &genericWorker{kind: kind, fn: fn})
	return nil
}

// genericWorker is the internal river worker that adapts a river job back
// to a consumer HandlerFunc. It unwraps the envelope and passes only the
// original payload, so consumer handlers never see river or envelope
// types.
type genericWorker struct {
	vendorriver.WorkerDefaults[genericArgs]
	kind string
	fn   queue.HandlerFunc
}

// Work is invoked by river's own dispatch loop when a live worker processes
// a job — only reachable with a running Start against a real database.
func (w *genericWorker) Work(ctx context.Context, job *vendorriver.Job[genericArgs]) error { // coverage-ignore
	return w.fn(ctx, job.Args.Payload)
}

// Start begins processing jobs in this process. It requires a live database
// and is a no-op when no queues were configured.
func (r *Enqueuer) Start(ctx context.Context) error { // coverage-ignore
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return queue.ErrAlreadyStarted
	}
	r.started = true
	r.mu.Unlock()
	return r.client.Start(ctx)
}

// Stop gracefully stops the worker. Safe to call once after Start.
func (r *Enqueuer) Stop(ctx context.Context) error { // coverage-ignore
	return r.client.Stop(ctx)
}
