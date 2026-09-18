// Package gocron is the production cron.Cron adapter backed by
// go-co-op/gocron/v2. With a cron.Locker configured it gates every tick, so
// only the lock holder runs a given job across replicas. Vendor types stay
// internal to this package.
package gocron

import (
	"context"
	"fmt"
	"sync"

	vendorgocron "github.com/go-co-op/gocron/v2"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
)

// Cron is the production cron.Cron backed by go-co-op/gocron/v2.
type Cron struct {
	mu      sync.Mutex
	sched   vendorgocron.Scheduler
	names   map[string]struct{}
	pending []pendingJob
	started bool
}

type pendingJob struct {
	spec, name string
	fn         cron.JobFunc
}

var _ cron.Cron = (*Cron)(nil)

// Config configures a Cron.
type Config struct {
	// Locker gates every tick so only the lock holder runs it across
	// replicas. Defaults to cron.NoopLocker, which does not coordinate
	// across replicas.
	Locker cron.Locker
}

// New builds a gocron-backed Cron. It constructs the underlying scheduler
// and returns an error if gocron rejects the configuration.
func New(cfg Config) (*Cron, error) {
	locker := cfg.Locker
	if locker == nil {
		locker = cron.NoopLocker{}
	}

	// Bridging cron.Locker onto gocron's own Locker lets gocron enforce
	// per-name exclusivity through the advisory-lock implementation.
	sched, err := vendorgocron.NewScheduler(
		vendorgocron.WithDistributedLocker(&lockerBridge{inner: locker}),
	)
	if err != nil {
		return nil, fmt.Errorf("cron/gocron: new scheduler: %w", err)
	}
	return &Cron{
		sched: sched,
		names: make(map[string]struct{}),
	}, nil
}

// Register validates and stores a cron job. Jobs are created on the
// underlying scheduler at Start. See cron.Cron.
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
	// Parse the spec eagerly so a bad one fails at registration, not at Start.
	if _, err := cron.ParseSpec(spec); err != nil {
		return fmt.Errorf("cron/gocron: parse spec %q: %w", spec, err)
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
	c.pending = append(c.pending, pendingJob{spec: spec, name: name, fn: fn})
	return nil
}

// Start creates the gocron jobs and starts the scheduler. gocron supplies
// each job run a context that is cancelled on Stop. See cron.Cron.
func (c *Cron) Start(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return cron.ErrAlreadyStarted
	}
	for _, p := range c.pending {
		_, err := c.sched.NewJob(
			vendorgocron.CronJob(p.spec, false),
			vendorgocron.NewTask(func(jobCtx context.Context) {
				// The distributed locker already gated this run. Errors
				// surface through gocron's event listeners; swallowing here
				// keeps a job fault from killing the scheduler goroutine.
				_ = p.fn(jobCtx)
			}),
			vendorgocron.WithName(p.name),
		)
		if err != nil {
			return fmt.Errorf("cron/gocron: create job %q: %w", p.name, err)
		}
	}
	c.sched.Start()
	c.started = true
	return nil
}

// Stop shuts the scheduler down. See cron.Cron.
func (c *Cron) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return nil
	}
	c.started = false
	if err := c.sched.ShutdownWithContext(ctx); err != nil {
		return fmt.Errorf("cron/gocron: shutdown: %w", err)
	}
	return nil
}

// lockerBridge adapts cron.Locker to gocron's own Locker, keeping gocron
// types out of this package's callers.
type lockerBridge struct {
	inner cron.Locker
}

var _ vendorgocron.Locker = (*lockerBridge)(nil)

func (b *lockerBridge) Lock(ctx context.Context, key string) (vendorgocron.Lock, error) {
	u, err := b.inner.Lock(ctx, key)
	if err != nil {
		return nil, err
	}
	return &lockBridge{u: u}, nil
}

type lockBridge struct {
	u cron.Unlocker
}

var _ vendorgocron.Lock = (*lockBridge)(nil)

func (b *lockBridge) Unlock(ctx context.Context) error {
	return b.u.Unlock(ctx)
}
