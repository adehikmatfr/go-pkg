// Package cron is the recurring-job scheduling port: a vendor-free Cron
// consumers depend on, guarded by a Locker so a tick runs on at most one
// replica. No third-party import — a concrete adapter (e.g.
// scheduler/cron/gocron) keeps its vendor types internal.
package cron

import (
	"context"
	"errors"
)

// Sentinel errors returned by every Cron/Locker implementation. Match them
// with errors.Is.
var (
	// ErrAlreadyStarted is returned by Start when called more than once, or
	// by Register when called after Start.
	ErrAlreadyStarted = errors.New("cron: already started")
	// ErrEmptyName is returned when a job is registered without a stable
	// name. The name is the advisory-lock key, so it must be non-empty.
	ErrEmptyName = errors.New("cron: empty job name")
	// ErrEmptySpec is returned when a job is registered with an empty
	// crontab spec.
	ErrEmptySpec = errors.New("cron: empty crontab spec")
	// ErrNilFunc is returned when a job is registered with a nil function.
	ErrNilFunc = errors.New("cron: nil job func")
	// ErrDuplicateName is returned when two jobs share a name within one
	// Cron instance.
	ErrDuplicateName = errors.New("cron: duplicate job name")
	// ErrLockHeld is returned by a Locker when the lock is already held,
	// telling the runner to skip this tick on this replica.
	ErrLockHeld = errors.New("cron: lock already held")
)

// JobFunc is a recurring task. It receives a context cancelled on shutdown.
type JobFunc func(ctx context.Context) error

// Cron is the port for recurring scheduled work.
type Cron interface {
	// Register schedules fn to run on the crontab spec under the stable
	// name. The name is the advisory-lock key: across replicas, only the
	// lock holder runs a given tick. Register before Start; registering
	// after Start returns ErrAlreadyStarted. Returns ErrDuplicateName for a
	// repeated name.
	Register(spec, name string, fn JobFunc) error

	// Start begins scheduling. It is non-blocking and may be called once.
	Start(ctx context.Context) error

	// Stop halts scheduling and releases resources. It is safe to call once
	// after Start.
	Stop(ctx context.Context) error
}

// Locker provides mutual exclusion for cron ticks across replicas.
//
// Lock is called with the cron job's name as key just before a tick runs; if
// it returns an error the tick is skipped on this replica. The returned
// Unlocker is invoked after the tick completes.
type Locker interface {
	// Lock attempts to acquire the named lock. A non-nil error means "not
	// acquired" and the tick is skipped.
	Lock(ctx context.Context, key string) (Unlocker, error)
}

// Unlocker releases a lock acquired via Locker.Lock.
type Unlocker interface {
	// Unlock releases the lock. It must be safe to call exactly once.
	Unlock(ctx context.Context) error
}
