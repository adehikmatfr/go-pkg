// Package memory is an in-memory queue.JobEnqueuer and queue.HandlerRegistry
// adapter for unit tests and single-process development: it records every
// enqueue, enforces idempotency by (kind, IdempotencyKey), makes EnqueueTx
// jobs visible only on commit (via Tx, this package's queue.TxHook-capable
// fake transaction), and dispatches recorded jobs through Run.
//
// It implements the same ports as scheduler/queue/river, which additionally
// provides cross-process durability and retries over a live database.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
)

// RecordedJob is a job captured by Enqueuer. It exposes the marshalled
// payload and the enqueue metadata so tests (and local tooling) can assert
// on what was enqueued without a database.
type RecordedJob struct {
	ID             int64
	Kind           string
	IdempotencyKey string
	Queue          string
	MaxAttempts    int
	Payload        []byte
	// Pending is true while the job was enqueued inside a transaction that
	// has not yet committed. EnqueueTx jobs start Pending; they become
	// visible (Pending=false) only when the transaction commits.
	Pending bool
}

// Enqueuer is an in-memory queue.JobEnqueuer and queue.HandlerRegistry.
type Enqueuer struct {
	mu       sync.Mutex
	nextID   int64
	jobs     []RecordedJob
	byKey    map[string]int // (kind\x00key) -> index into jobs
	handlers map[string]queue.HandlerFunc
}

// New returns a ready in-memory Enqueuer.
func New() *Enqueuer {
	return &Enqueuer{
		byKey:    make(map[string]int),
		handlers: make(map[string]queue.HandlerFunc),
	}
}

var (
	_ queue.JobEnqueuer     = (*Enqueuer)(nil)
	_ queue.HandlerRegistry = (*Enqueuer)(nil)
)

func idemKey(kind, key string) string { return kind + "\x00" + key }

// Enqueue records the job immediately (Pending=false). See
// queue.JobEnqueuer.
func (m *Enqueuer) Enqueue(_ context.Context, job queue.Job) (queue.EnqueueResult, error) {
	return m.record(job, false)
}

// EnqueueTx records the job as Pending and registers a commit/rollback
// callback on the transaction so the job is made visible on commit and
// dropped on rollback. See queue.JobEnqueuer.
//
// The tx must implement queue.TxHook (this package's Tx does). A plain Tx
// without TxHook is treated as auto-commit so the contract degrades
// gracefully.
func (m *Enqueuer) EnqueueTx(_ context.Context, tx queue.Tx, job queue.Job) (queue.EnqueueResult, error) {
	if tx == nil {
		return queue.EnqueueResult{}, queue.ErrNilTx
	}
	hook, transactional := tx.(queue.TxHook)
	res, err := m.record(job, transactional)
	if err != nil {
		return res, err
	}
	if transactional && !res.AlreadyExisted {
		id := res.ID
		hook.OnCommit(func() { m.setPending(id, false) })
		hook.OnRollback(func() { m.remove(id) })
	}
	return res, nil
}

func (m *Enqueuer) record(job queue.Job, pending bool) (queue.EnqueueResult, error) {
	if err := queue.ValidateJob(job); err != nil {
		return queue.EnqueueResult{}, err
	}
	payload, err := json.Marshal(job.Args)
	if err != nil {
		return queue.EnqueueResult{}, fmt.Errorf("queue/memory: marshal job %q: %w", job.Args.Kind(), err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	kind := job.Args.Kind()
	if job.IdempotencyKey != "" {
		if idx, ok := m.byKey[idemKey(kind, job.IdempotencyKey)]; ok {
			existing := m.jobs[idx]
			return queue.EnqueueResult{ID: existing.ID, Kind: kind, AlreadyExisted: true}, nil
		}
	}

	m.nextID++
	rec := RecordedJob{
		ID:             m.nextID,
		Kind:           kind,
		IdempotencyKey: job.IdempotencyKey,
		Queue:          job.Queue,
		MaxAttempts:    job.MaxAttempts,
		Payload:        payload,
		Pending:        pending,
	}
	m.jobs = append(m.jobs, rec)
	if job.IdempotencyKey != "" {
		m.byKey[idemKey(kind, job.IdempotencyKey)] = len(m.jobs) - 1
	}
	return queue.EnqueueResult{ID: rec.ID, Kind: kind, AlreadyExisted: false}, nil
}

func (m *Enqueuer) setPending(id int64, pending bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.jobs {
		if m.jobs[i].ID == id {
			m.jobs[i].Pending = pending
			return
		}
	}
}

func (m *Enqueuer) remove(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.jobs {
		if m.jobs[i].ID == id {
			if m.jobs[i].IdempotencyKey != "" {
				delete(m.byKey, idemKey(m.jobs[i].Kind, m.jobs[i].IdempotencyKey))
			}
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			// Rebuild byKey indexes since slice positions shifted.
			m.reindexLocked()
			return
		}
	}
}

func (m *Enqueuer) reindexLocked() {
	m.byKey = make(map[string]int, len(m.jobs))
	for i := range m.jobs {
		if m.jobs[i].IdempotencyKey != "" {
			m.byKey[idemKey(m.jobs[i].Kind, m.jobs[i].IdempotencyKey)] = i
		}
	}
}

// RegisterHandler binds fn to kind. See queue.HandlerRegistry.
func (m *Enqueuer) RegisterHandler(kind string, fn queue.HandlerFunc) error {
	if kind == "" {
		return queue.ErrEmptyKind
	}
	if fn == nil {
		return fmt.Errorf("queue/memory: nil handler func")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.handlers[kind]; dup {
		return fmt.Errorf("queue/memory: handler already registered for kind %q", kind)
	}
	m.handlers[kind] = fn
	return nil
}

// Jobs returns a snapshot of recorded jobs in enqueue order.
func (m *Enqueuer) Jobs() []RecordedJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RecordedJob, len(m.jobs))
	copy(out, m.jobs)
	return out
}

// VisibleJobs returns recorded jobs that are not Pending (i.e. committed).
func (m *Enqueuer) VisibleJobs() []RecordedJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RecordedJob
	for _, j := range m.jobs {
		if !j.Pending {
			out = append(out, j)
		}
	}
	return out
}

// Run dispatches every visible recorded job to its registered handler, in
// order, and returns the first handler error. Pending (uncommitted) jobs are
// skipped. A job whose kind has no handler returns an error. This simulates
// a worker drain for tests and local runs.
func (m *Enqueuer) Run(ctx context.Context) error {
	for _, j := range m.VisibleJobs() {
		m.mu.Lock()
		fn, ok := m.handlers[j.Kind]
		m.mu.Unlock()
		if !ok {
			return fmt.Errorf("queue/memory: no handler registered for kind %q", j.Kind)
		}
		if err := fn(ctx, j.Payload); err != nil {
			return fmt.Errorf("queue/memory: run job %q (id=%d): %w", j.Kind, j.ID, err)
		}
	}
	return nil
}
