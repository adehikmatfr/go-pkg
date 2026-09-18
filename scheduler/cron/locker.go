package cron

import (
	"context"
	"sync"
)

// NoopLocker is a Locker that always acquires. Use it for single-replica
// deployments where cron exclusivity across processes is not required. It is
// the sensible default when no Locker is configured.
type NoopLocker struct{}

// NewNoopLocker returns a Locker that grants every lock immediately.
func NewNoopLocker() Locker { return NoopLocker{} }

// Lock always succeeds and returns a no-op Unlocker.
func (NoopLocker) Lock(context.Context, string) (Unlocker, error) {
	return noopUnlocker{}, nil
}

type noopUnlocker struct{}

func (noopUnlocker) Unlock(context.Context) error { return nil }

// InMemoryLocker is a process-local Locker: within one process at most one
// holder runs a given key at a time. It does not coordinate across
// processes, so multi-replica exclusivity needs a distributed locker (e.g. a
// Postgres advisory lock).
type InMemoryLocker struct {
	mu   sync.Mutex
	held map[string]struct{}
}

// NewInMemoryLocker returns a ready-to-use process-local Locker.
func NewInMemoryLocker() *InMemoryLocker {
	return &InMemoryLocker{held: make(map[string]struct{})}
}

// Lock acquires key if it is free. If key is already held, it returns
// ErrLockHeld and the caller (the cron runner) skips this tick.
func (l *InMemoryLocker) Lock(_ context.Context, key string) (Unlocker, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held == nil {
		l.held = make(map[string]struct{})
	}
	if _, ok := l.held[key]; ok {
		return nil, ErrLockHeld
	}
	l.held[key] = struct{}{}
	return &inMemoryUnlock{l: l, key: key}, nil
}

type inMemoryUnlock struct {
	l    *InMemoryLocker
	key  string
	once sync.Once
}

func (u *inMemoryUnlock) Unlock(context.Context) error {
	u.once.Do(func() {
		u.l.mu.Lock()
		delete(u.l.held, u.key)
		u.l.mu.Unlock()
	})
	return nil
}
