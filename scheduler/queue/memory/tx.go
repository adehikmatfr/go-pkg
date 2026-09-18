package memory

import (
	"context"
	"sync"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
)

// Tx is an in-memory queue.Tx and queue.TxHook that exercises the
// EnqueueTx contract, including commit/rollback visibility of enqueued
// jobs, without a database.
//
// It is not a general-purpose SQL transaction: ExecContext records calls
// and reports zero rows affected. Production uses queue.NewSQLTx with a
// SQL-backed adapter (e.g. scheduler/queue/river).
type Tx struct {
	mu        sync.Mutex
	committed bool
	rolled    bool
	onCommit  []func()
	onRoll    []func()
	// Execs records the queries run within the transaction, for assertions.
	Execs []string
}

// NewTx returns an open in-memory transaction.
func NewTx() *Tx { return &Tx{} }

var (
	_ queue.Tx     = (*Tx)(nil)
	_ queue.TxHook = (*Tx)(nil)
)

// ExecContext records the query and returns a zero-rows Result. See
// queue.Tx.
func (t *Tx) ExecContext(_ context.Context, query string, _ ...any) (queue.Result, error) {
	t.mu.Lock()
	t.Execs = append(t.Execs, query)
	t.mu.Unlock()
	return zeroResult{}, nil
}

// OnCommit registers a callback fired by Commit. See queue.TxHook.
func (t *Tx) OnCommit(fn func()) {
	t.mu.Lock()
	t.onCommit = append(t.onCommit, fn)
	t.mu.Unlock()
}

// OnRollback registers a callback fired by Rollback. See queue.TxHook.
func (t *Tx) OnRollback(fn func()) {
	t.mu.Lock()
	t.onRoll = append(t.onRoll, fn)
	t.mu.Unlock()
}

// Commit fires every OnCommit callback once, making jobs enqueued through
// EnqueueTx on this transaction visible. It is idempotent: a closed tx is a
// no-op.
func (t *Tx) Commit() {
	t.mu.Lock()
	if t.committed || t.rolled {
		t.mu.Unlock()
		return
	}
	t.committed = true
	cbs := append([]func(){}, t.onCommit...)
	t.mu.Unlock()
	for _, fn := range cbs {
		fn()
	}
}

// Rollback fires every OnRollback callback once, dropping jobs enqueued
// through EnqueueTx on this transaction. It is idempotent.
func (t *Tx) Rollback() {
	t.mu.Lock()
	if t.committed || t.rolled {
		t.mu.Unlock()
		return
	}
	t.rolled = true
	cbs := append([]func(){}, t.onRoll...)
	t.mu.Unlock()
	for _, fn := range cbs {
		fn()
	}
}

type zeroResult struct{}

func (zeroResult) RowsAffected() (int64, error) { return 0, nil }
