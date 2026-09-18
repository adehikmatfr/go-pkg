package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron/memory"
)

// fixedClock is a memory.Clock that can be advanced explicitly.
type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFixedClock(t time.Time) *fixedClock { return &fixedClock{t: t} }

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fixedClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestRegister_Validation(t *testing.T) {
	c := memory.New(nil)

	tests := []struct {
		name    string
		spec    string
		jobName string
		fn      cron.JobFunc
		wantErr error
	}{
		{name: "empty name", spec: "* * * * *", jobName: "", fn: func(context.Context) error { return nil }, wantErr: cron.ErrEmptyName},
		{name: "empty spec", spec: "", jobName: "job", fn: func(context.Context) error { return nil }, wantErr: cron.ErrEmptySpec},
		{name: "nil func", spec: "* * * * *", jobName: "job", fn: nil, wantErr: cron.ErrNilFunc},
		{name: "invalid spec", spec: "bad", jobName: "job", fn: func(context.Context) error { return nil }, wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Register(tt.spec, tt.jobName, tt.fn)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.name == "invalid spec" {
				if err == nil {
					t.Fatalf("expected a parse error for an invalid spec")
				}
				return
			}
		})
	}
}

func TestRegister_DuplicateName(t *testing.T) {
	c := memory.New(nil)
	noop := func(context.Context) error { return nil }
	if err := c.Register("* * * * *", "job", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Register("* * * * *", "job", noop); !errors.Is(err, cron.ErrDuplicateName) {
		t.Fatalf("got err %v, want ErrDuplicateName", err)
	}
}

func TestRegister_AfterStart(t *testing.T) {
	c := memory.New(nil)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := c.Register("* * * * *", "job", func(context.Context) error { return nil })
	if !errors.Is(err, cron.ErrAlreadyStarted) {
		t.Fatalf("got err %v, want ErrAlreadyStarted", err)
	}
}

func TestStart_Twice(t *testing.T) {
	c := memory.New(nil)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := c.Start(context.Background()); !errors.Is(err, cron.ErrAlreadyStarted) {
		t.Fatalf("got err %v, want ErrAlreadyStarted", err)
	}
}

func TestAdvance_BeforeStartIsNoop(t *testing.T) {
	c := memory.New(nil)
	ran, err := c.Advance(context.Background())
	if err != nil || ran != 0 {
		t.Fatalf("Advance() before Start = (%d, %v), want (0, nil)", ran, err)
	}
}

func TestAdvance_RunsDueJobs(t *testing.T) {
	clock := newFixedClock(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC))
	c := memory.New(clock)

	var mu sync.Mutex
	var runs int
	if err := c.Register("* * * * *", "every-minute", func(context.Context) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Not due yet: no time has passed since Start primed next-fire.
	ran, err := c.Advance(context.Background())
	if err != nil || ran != 0 {
		t.Fatalf("Advance() immediately after Start = (%d, %v), want (0, nil)", ran, err)
	}

	clock.Advance(time.Minute)
	ran, err = c.Advance(context.Background())
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if ran != 1 {
		t.Fatalf("Advance() ran = %d, want 1", ran)
	}
	mu.Lock()
	got := runs
	mu.Unlock()
	if got != 1 {
		t.Fatalf("job ran %d times, want 1", got)
	}

	// Rolls forward: advancing again by less than a minute should not re-fire.
	ran, err = c.Advance(context.Background())
	if err != nil || ran != 0 {
		t.Fatalf("Advance() with no new due time = (%d, %v), want (0, nil)", ran, err)
	}
}

func TestAdvance_JobError(t *testing.T) {
	clock := newFixedClock(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC))
	c := memory.New(clock)
	jobErr := errors.New("boom")

	if err := c.Register("* * * * *", "failing", func(context.Context) error { return jobErr }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	clock.Advance(time.Minute)

	ran, err := c.Advance(context.Background())
	if ran != 1 {
		t.Fatalf("Advance() ran = %d, want 1 (job still counts as attempted)", ran)
	}
	if !errors.Is(err, jobErr) {
		t.Fatalf("Advance() err = %v, want it to wrap %v", err, jobErr)
	}
}

func TestAdvance_LockSkipped(t *testing.T) {
	clock := newFixedClock(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC))
	locker := cron.NewInMemoryLocker()
	// Pre-hold the lock for "job" so every tick is skipped.
	if _, err := locker.Lock(context.Background(), "job"); err != nil {
		t.Fatalf("pre-Lock: %v", err)
	}

	c := memory.New(clock, memory.WithLocker(locker))
	var ran32 int
	if err := c.Register("* * * * *", "job", func(context.Context) error {
		ran32++
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	clock.Advance(time.Minute)

	ran, err := c.Advance(context.Background())
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if ran != 0 {
		t.Fatalf("Advance() ran = %d, want 0 (tick should be skipped: lock held)", ran)
	}
	if ran32 != 0 {
		t.Fatalf("job function was called despite the lock being held")
	}
}

func TestStop(t *testing.T) {
	clock := newFixedClock(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC))
	c := memory.New(clock)
	if err := c.Register("* * * * *", "job", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	clock.Advance(time.Minute)
	ran, err := c.Advance(context.Background())
	if err != nil || ran != 0 {
		t.Fatalf("Advance() after Stop = (%d, %v), want (0, nil)", ran, err)
	}
}

func TestNames(t *testing.T) {
	c := memory.New(nil)
	noop := func(context.Context) error { return nil }
	if err := c.Register("* * * * *", "b", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Register("* * * * *", "a", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := c.Names()
	want := []string{"b", "a"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Names() = %v, want %v (registration order)", got, want)
	}
}
