package queue_test

import (
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/queue"
)

type testArgs struct {
	kind string
}

func (a testArgs) Kind() string { return a.kind }

func TestNewJob(t *testing.T) {
	scheduledAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	job := queue.NewJob(testArgs{kind: "email_send"},
		queue.WithIdempotencyKey("key-1"),
		queue.WithQueue("high"),
		queue.WithMaxAttempts(5),
		queue.WithScheduledAt(scheduledAt),
	)

	if job.Args.Kind() != "email_send" {
		t.Errorf("Args.Kind() = %q, want %q", job.Args.Kind(), "email_send")
	}
	if job.IdempotencyKey != "key-1" {
		t.Errorf("IdempotencyKey = %q, want %q", job.IdempotencyKey, "key-1")
	}
	if job.Queue != "high" {
		t.Errorf("Queue = %q, want %q", job.Queue, "high")
	}
	if job.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", job.MaxAttempts)
	}
	if !job.ScheduledAt.Equal(scheduledAt) {
		t.Errorf("ScheduledAt = %v, want %v", job.ScheduledAt, scheduledAt)
	}
}

func TestValidateJob(t *testing.T) {
	tests := []struct {
		name    string
		job     queue.Job
		wantErr error
	}{
		{name: "nil args", job: queue.Job{}, wantErr: queue.ErrNilJob},
		{name: "empty kind", job: queue.Job{Args: testArgs{kind: ""}}, wantErr: queue.ErrEmptyKind},
		{name: "valid", job: queue.Job{Args: testArgs{kind: "email_send"}}, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := queue.ValidateJob(tt.job); !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLTx(t *testing.T) {
	// A live *sql.Tx needs a real connection (db.BeginTx dials); NewSQLTx and
	// Unwrap only need a *sql.Tx-shaped value to prove their identity
	// contract, so nil stands in here. ExecContext itself needs a live
	// connection and is exercised at the adapter level instead.
	sqlTx := queue.NewSQLTx(nil)
	if sqlTx.Unwrap() != nil {
		t.Fatalf("Unwrap() = %v, want nil for a Tx wrapping nil", sqlTx.Unwrap())
	}

	var _ queue.Tx = sqlTx
}
