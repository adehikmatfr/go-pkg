package memory_test

import (
	"context"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox/memory"
)

func TestTx_ExecContext_RecordsQueries(t *testing.T) {
	tx := memory.NewTx()
	res, err := tx.ExecContext(context.Background(), "INSERT INTO x VALUES (1)")
	if err != nil {
		t.Fatalf("ExecContext: %v", err)
	}
	if len(tx.Execs) != 1 || tx.Execs[0] != "INSERT INTO x VALUES (1)" {
		t.Fatalf("Execs = %v, want the recorded query", tx.Execs)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 0 {
		t.Fatalf("RowsAffected() = (%d, %v), want (0, nil)", n, err)
	}
}

func TestTx_Commit_Idempotent(t *testing.T) {
	tx := memory.NewTx()
	calls := 0
	tx.OnCommit(func() { calls++ })

	tx.Commit()
	tx.Commit()

	if calls != 1 {
		t.Fatalf("OnCommit callback fired %d times, want 1", calls)
	}
}

func TestTx_Rollback_Idempotent(t *testing.T) {
	tx := memory.NewTx()
	calls := 0
	tx.OnRollback(func() { calls++ })

	tx.Rollback()
	tx.Rollback()

	if calls != 1 {
		t.Fatalf("OnRollback callback fired %d times, want 1", calls)
	}
}

func TestTx_CommitAfterRollbackIsNoop(t *testing.T) {
	tx := memory.NewTx()
	commitCalls, rollCalls := 0, 0
	tx.OnCommit(func() { commitCalls++ })
	tx.OnRollback(func() { rollCalls++ })

	tx.Rollback()
	tx.Commit()

	if rollCalls != 1 || commitCalls != 0 {
		t.Fatalf("got commitCalls=%d rollCalls=%d, want 0/1 (a closed tx cannot also commit)", commitCalls, rollCalls)
	}
}
