// Package memory is a deterministic, clock-driven cron.Cron adapter for
// tests and single-process deployments driven by an external ticker. It
// parses crontab specs with cron.ParseSpec (the same parser
// scheduler/cron/gocron eagerly validates against) but decides whether a job
// is due from an injected Clock and an explicit Advance call rather than
// wall-clock timers, so recurring logic and the lock-skip path are testable
// without real time.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
)

// Clock returns the current time, so tests can drive Cron deterministically.
// Package-local, matching the security/otp precedent, rather than a shared
// clock dependency for the few packages that actually need one.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Cron is a deterministic cron.Cron. Each due tick acquires the job name as
// a lock and skips when it cannot, with the same skip semantics as
// scheduler/cron/gocron.
type Cron struct {
	clock  Clock
	locker cron.Locker

	mu      sync.Mutex
	jobs    []*job
	names   map[string]struct{}
	started bool
	stopped bool
}

type job struct {
	name     string
	spec     string
	fn       cron.JobFunc
	schedule cron.Schedule
	// next is the next fire time at/after which the job is due.
	next int64 // unix nanos; only meaningful once set is true
	set  bool
}

var _ cron.Cron = (*Cron)(nil)

// Option configures a Cron.
type Option func(*Cron)

// WithLocker sets the Locker used to gate ticks. Defaults to
// cron.NoopLocker.
func WithLocker(l cron.Locker) Option {
	return func(c *Cron) {
		if l != nil {
			c.locker = l
		}
	}
}

// New builds a deterministic Cron driven by clock. A nil clock uses the
// system clock. Specs are standard 5-field crontab (no seconds).
func New(clock Clock, opts ...Option) *Cron {
	if clock == nil {
		clock = systemClock{}
	}
	c := &Cron{
		clock:  clock,
		locker: cron.NoopLocker{},
		names:  make(map[string]struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Register validates and stores a cron job. See cron.Cron.
func (c *Cron) Register(spec, name string, fn cron.JobFunc) error {
	if name == "" {
		return cron.ErrEmptyName
	}
	if spec == "" {
		return cron.ErrEmptySpec
	}
	if fn == nil {
		return cron.ErrNilFunc
	}
	sched, err := cron.ParseSpec(spec)
	if err != nil {
		return fmt.Errorf("cron/memory: parse spec %q: %w", spec, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return cron.ErrAlreadyStarted
	}
	if _, dup := c.names[name]; dup {
		return cron.ErrDuplicateName
	}
	c.names[name] = struct{}{}
	c.jobs = append(c.jobs, &job{name: name, spec: spec, fn: fn, schedule: sched})
	return nil
}

// Start marks the cron running and primes each job's next fire time
// relative to the current clock. It does not spin any goroutine: callers
// drive progress with Advance. See cron.Cron.
func (c *Cron) Start(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return cron.ErrAlreadyStarted
	}
	now := c.clock.Now()
	for _, j := range c.jobs {
		j.next = j.schedule.Next(now).UnixNano()
		j.set = true
	}
	c.started = true
	return nil
}

// Stop marks the cron stopped. See cron.Cron.
func (c *Cron) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	c.started = false
	return nil
}

// errLockSkipped is an internal sentinel: the tick was skipped because the
// lock was not acquired. It never escapes the package.
var errLockSkipped = fmt.Errorf("cron/memory: tick skipped: lock not acquired")

// Advance runs every job whose next fire time the clock has reached, rolling
// each schedule forward and running a job at most once per call. It returns
// how many jobs executed and the first error; a tick that cannot acquire its
// lock is skipped and not counted. Drive it from a test clock or a ticker
// loop; it is a no-op before Start or after Stop.
func (c *Cron) Advance(ctx context.Context) (ran int, firstErr error) {
	c.mu.Lock()
	if !c.started || c.stopped {
		c.mu.Unlock()
		return 0, nil
	}
	now := c.clock.Now()
	nowNanos := now.UnixNano()

	var due []*job
	for _, j := range c.jobs {
		if j.set && j.next <= nowNanos {
			due = append(due, j)
			// Roll forward to the next fire strictly after now.
			j.next = j.schedule.Next(now).UnixNano()
		}
	}
	c.mu.Unlock()

	for _, j := range due {
		if err := c.runOne(ctx, j); err != nil {
			if err == errLockSkipped { //nolint:errorlint // internal sentinel, never wrapped
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		ran++
	}
	return ran, firstErr
}

func (c *Cron) runOne(ctx context.Context, j *job) error {
	unlock, err := c.locker.Lock(ctx, j.name)
	if err != nil {
		return errLockSkipped
	}
	defer func() { _ = unlock.Unlock(ctx) }()
	if runErr := j.fn(ctx); runErr != nil {
		return fmt.Errorf("cron/memory: job %q: %w", j.name, runErr)
	}
	return nil
}

// Names returns the registered cron job names in registration order. Useful
// for assertions and operational introspection.
func (c *Cron) Names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.jobs))
	for _, j := range c.jobs {
		out = append(out, j.name)
	}
	return out
}
