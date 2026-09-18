package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue/memory"
)

type emailArgs struct {
	Kind_ string `json:"-"`
	To    string `json:"to"`
}

func (a emailArgs) Kind() string { return a.Kind_ }

func job(to, idemKey string) queue.Job {
	return queue.NewJob(emailArgs{Kind_: "email_send", To: to}, queue.WithIdempotencyKey(idemKey))
}

func TestEnqueue(t *testing.T) {
	e := memory.New()
	res, err := e.Enqueue(context.Background(), job("a@example.com", ""))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if res.Kind != "email_send" || res.AlreadyExisted {
		t.Fatalf("unexpected result: %+v", res)
	}

	jobs := e.Jobs()
	if len(jobs) != 1 || jobs[0].Pending {
		t.Fatalf("expected one non-pending recorded job, got %+v", jobs)
	}
	visible := e.VisibleJobs()
	if len(visible) != 1 {
		t.Fatalf("VisibleJobs() = %d, want 1", len(visible))
	}
}

func TestEnqueue_InvalidJob(t *testing.T) {
	e := memory.New()
	if _, err := e.Enqueue(context.Background(), queue.Job{}); !errors.Is(err, queue.ErrNilJob) {
		t.Fatalf("got err %v, want ErrNilJob", err)
	}
}

func TestEnqueue_Idempotent(t *testing.T) {
	e := memory.New()
	res1, err := e.Enqueue(context.Background(), job("a@example.com", "key-1"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	res2, err := e.Enqueue(context.Background(), job("a@example.com", "key-1"))
	if err != nil {
		t.Fatalf("re-Enqueue: %v", err)
	}
	if !res2.AlreadyExisted || res2.ID != res1.ID {
		t.Fatalf("got %+v, want AlreadyExisted=true with the same ID as %+v", res2, res1)
	}
	if len(e.Jobs()) != 1 {
		t.Fatalf("Jobs() = %d, want 1 (no duplicate)", len(e.Jobs()))
	}
}

func TestEnqueueTx_NilTx(t *testing.T) {
	e := memory.New()
	if _, err := e.EnqueueTx(context.Background(), nil, job("a@example.com", "")); !errors.Is(err, queue.ErrNilTx) {
		t.Fatalf("got err %v, want ErrNilTx", err)
	}
}

func TestEnqueueTx_VisibleOnCommit(t *testing.T) {
	e := memory.New()
	tx := memory.NewTx()

	res, err := e.EnqueueTx(context.Background(), tx, job("a@example.com", ""))
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}

	jobs := e.Jobs()
	if len(jobs) != 1 || !jobs[0].Pending {
		t.Fatalf("expected one Pending job before commit, got %+v", jobs)
	}
	if len(e.VisibleJobs()) != 0 {
		t.Fatalf("VisibleJobs() before commit = %d, want 0", len(e.VisibleJobs()))
	}

	tx.Commit()

	visible := e.VisibleJobs()
	if len(visible) != 1 || visible[0].ID != res.ID {
		t.Fatalf("VisibleJobs() after commit = %+v, want the committed job visible", visible)
	}
}

func TestEnqueueTx_DroppedOnRollback(t *testing.T) {
	e := memory.New()
	tx := memory.NewTx()

	if _, err := e.EnqueueTx(context.Background(), tx, job("a@example.com", "")); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	tx.Rollback()

	if len(e.Jobs()) != 0 {
		t.Fatalf("Jobs() after rollback = %d, want 0 (dropped)", len(e.Jobs()))
	}
}

func TestEnqueueTx_RollbackReindexesIdempotencyKeys(t *testing.T) {
	e := memory.New()

	// A committed job with an idempotency key stays in the index...
	if _, err := e.Enqueue(context.Background(), job("a@example.com", "key-a")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// ...while a second, rolled-back job (also keyed) must be fully removed,
	// including its byKey entry, so the slice/byKey indexes stay consistent.
	tx := memory.NewTx()
	if _, err := e.EnqueueTx(context.Background(), tx, job("b@example.com", "key-b")); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	tx.Rollback()

	if len(e.Jobs()) != 1 {
		t.Fatalf("Jobs() after rollback = %d, want 1 (only the committed job remains)", len(e.Jobs()))
	}

	// key-b is free again: enqueuing it fresh must not be treated as a
	// duplicate of the rolled-back job.
	res, err := e.Enqueue(context.Background(), job("c@example.com", "key-b"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if res.AlreadyExisted {
		t.Fatalf("got AlreadyExisted=true, want false (rolled-back key must be reusable)")
	}
}

func TestEnqueueTx_NonTxHookTreatedAsAutoCommit(t *testing.T) {
	e := memory.New()
	// A Tx that doesn't implement queue.TxHook (queue.SQLTx wraps nil here,
	// since it never registers OnCommit/OnRollback either).
	tx := queue.NewSQLTx(nil)
	res, err := e.EnqueueTx(context.Background(), tx, job("a@example.com", ""))
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	visible := e.VisibleJobs()
	if len(visible) != 1 || visible[0].ID != res.ID {
		t.Fatalf("expected the job to be immediately visible (auto-commit), got %+v", visible)
	}
}

func TestRegisterHandler(t *testing.T) {
	e := memory.New()
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

func TestRun(t *testing.T) {
	e := memory.New()
	var received []string
	if err := e.RegisterHandler("email_send", func(_ context.Context, raw []byte) error {
		received = append(received, string(raw))
		return nil
	}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	if _, err := e.Enqueue(context.Background(), job("a@example.com", "")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// A pending (uncommitted) job must not be dispatched.
	tx := memory.NewTx()
	if _, err := e.EnqueueTx(context.Background(), tx, job("b@example.com", "")); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}

	if err := e.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("Run() dispatched %d jobs, want 1 (the pending one must be skipped)", len(received))
	}
}

func TestRun_NoHandlerRegistered(t *testing.T) {
	e := memory.New()
	if _, err := e.Enqueue(context.Background(), job("a@example.com", "")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := e.Run(context.Background()); err == nil {
		t.Fatalf("expected an error for a job with no registered handler")
	}
}

func TestRun_HandlerError(t *testing.T) {
	e := memory.New()
	handlerErr := errors.New("boom")
	if err := e.RegisterHandler("email_send", func(context.Context, []byte) error { return handlerErr }); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if _, err := e.Enqueue(context.Background(), job("a@example.com", "")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := e.Run(context.Background()); !errors.Is(err, handlerErr) {
		t.Fatalf("got err %v, want it to wrap %v", err, handlerErr)
	}
}
