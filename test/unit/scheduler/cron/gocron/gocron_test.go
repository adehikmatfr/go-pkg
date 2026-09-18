package gocron_test

import (
	"context"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron/gocron"
)

func TestNew(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatalf("expected a non-nil Cron")
	}
}

func TestRegister_Validation(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	noop := func(context.Context) error { return nil }

	tests := []struct {
		name    string
		spec    string
		jobName string
		fn      cron.JobFunc
		wantErr error
	}{
		{name: "empty name", spec: "* * * * *", jobName: "", fn: noop, wantErr: cron.ErrEmptyName},
		{name: "empty spec", spec: "", jobName: "job", fn: noop, wantErr: cron.ErrEmptySpec},
		{name: "nil func", spec: "* * * * *", jobName: "job", fn: nil, wantErr: cron.ErrNilFunc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := c.Register(tt.spec, tt.jobName, tt.fn); !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegister_InvalidSpec(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Register("not-a-spec", "job", func(context.Context) error { return nil }); err == nil {
		t.Fatalf("expected an error for an invalid crontab spec")
	}
}

func TestRegister_DuplicateName(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	noop := func(context.Context) error { return nil }
	if err := c.Register("* * * * *", "job", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Register("* * * * *", "job", noop); !errors.Is(err, cron.ErrDuplicateName) {
		t.Fatalf("got err %v, want ErrDuplicateName", err)
	}
}

func TestRegister_AfterStart(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = c.Stop(context.Background()) }()

	err = c.Register("* * * * *", "job", func(context.Context) error { return nil })
	if !errors.Is(err, cron.ErrAlreadyStarted) {
		t.Fatalf("got err %v, want ErrAlreadyStarted", err)
	}
}

func TestStart_Twice(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer func() { _ = c.Stop(context.Background()) }()

	if err := c.Start(context.Background()); !errors.Is(err, cron.ErrAlreadyStarted) {
		t.Fatalf("got err %v, want ErrAlreadyStarted", err)
	}
}

func TestStop_BeforeStartIsNoop(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before Start = %v, want nil", err)
	}
}

func TestStop_Idempotent(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestStart_CreatesJobsFromPending(t *testing.T) {
	c, err := gocron.New(gocron.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Register("* * * * *", "job", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A real tick (up to 60s away for a 5-field spec) is not worth waiting for
	// in a unit test; this asserts Start wires the pending job into gocron
	// without error, which is what Register/Start actually own in this
	// adapter — the tick-execution semantics (lock-skip, error handling) are
	// covered end-to-end by scheduler/cron/memory, which shares the same
	// cron.ParseSpec/Schedule engine.
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
