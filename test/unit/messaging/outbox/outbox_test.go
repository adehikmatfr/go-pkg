package outbox_test

import (
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
)

func TestEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   outbox.Event
		wantErr error
	}{
		{name: "missing id", event: outbox.Event{Topic: "orders"}, wantErr: outbox.ErrMissingID},
		{name: "missing topic", event: outbox.Event{ID: "evt-1"}, wantErr: outbox.ErrMissingTopic},
		{name: "valid", event: outbox.Event{ID: "evt-1", Topic: "orders"}, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 8 * time.Second},
		{attempt: 20, want: time.Hour}, // capped
		{attempt: 100, want: time.Hour},
	}
	for _, tt := range tests {
		got := outbox.DefaultBackoff(tt.attempt)
		if got != tt.want {
			t.Errorf("DefaultBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestSQLTx(t *testing.T) {
	// A live *sql.Tx needs a real connection (db.BeginTx dials); NewSQLTx and
	// Unwrap only need a *sql.Tx-shaped value to prove their identity
	// contract, so nil stands in here. ExecContext itself needs a live
	// connection and is exercised at the adapter level instead.
	sqlTx := outbox.NewSQLTx(nil)
	if sqlTx.Unwrap() != nil {
		t.Fatalf("Unwrap() = %v, want nil for a Tx wrapping nil", sqlTx.Unwrap())
	}
	var _ outbox.Tx = sqlTx
}
