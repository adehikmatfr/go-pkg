package otp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Errors returned by ChallengeStore implementations.
var (
	// ErrChallengeNotFound is returned when no live challenge exists for an
	// id.
	ErrChallengeNotFound = errors.New("otp: challenge not found")
	// ErrChallengeExpired is returned when a challenge exists but its TTL
	// has elapsed. Expired challenges are treated as consumed and removed.
	ErrChallengeExpired = errors.New("otp: challenge expired")
	// ErrInvalidChallenge is returned by Put for an empty id or non-positive
	// TTL.
	ErrInvalidChallenge = errors.New("otp: invalid challenge")
)

// Challenge is a single pending verification/reset challenge: an opaque id,
// the hash of the issued code (never the plaintext — see CodeGenerator.Hash),
// and metadata. The store stamps ExpiresAt on Put; callers leave it zero.
type Challenge struct {
	// ID uniquely identifies the challenge (e.g. a UUID handed to the
	// client).
	ID string
	// CodeHash is the hash of the issued code (from CodeGenerator.Hash). The
	// plaintext code is delivered out-of-band and never stored.
	CodeHash string
	// Purpose labels the challenge (e.g. "login", "password_reset") so a
	// code issued for one flow cannot be replayed in another.
	Purpose string
	// Destination is an optional opaque target reference (e.g. a masked
	// phone or email id).
	Destination string
	// ExpiresAt is set by the store on Put. Zero on input.
	ExpiresAt time.Time
}

// ChallengeStore persists single-use, TTL'd verification challenges.
//
// An implementation enforces single use: a successful Consume removes the
// challenge, so a code cannot be replayed.
type ChallengeStore interface {
	// Put stores ch with the given TTL, stamping its expiry. An existing
	// challenge with the same id is replaced. It returns
	// ErrInvalidChallenge for an empty id or non-positive ttl.
	Put(ctx context.Context, ch Challenge, ttl time.Duration) error
	// Get returns the live challenge for id: ErrChallengeNotFound if
	// absent, ErrChallengeExpired (removing the entry) if past its TTL. It
	// does not consume the challenge.
	Get(ctx context.Context, id string) (Challenge, error)
	// Consume atomically validates that a live challenge exists for id and
	// removes it, enforcing single use. The caller verifies the code
	// against the hash Get returned.
	Consume(ctx context.Context, id string) error
}

// memoryChallengeStore is a mutex-guarded ChallengeStore for single-process
// use and tests.
type memoryChallengeStore struct {
	clock Clock
	mu    sync.Mutex
	items map[string]Challenge
}

// NewMemoryChallengeStore returns an in-memory ChallengeStore using clock for
// TTL evaluation. A nil clock uses time.Now.
func NewMemoryChallengeStore(clock Clock) ChallengeStore {
	if clock == nil {
		clock = systemClock{}
	}
	return &memoryChallengeStore{
		clock: clock,
		items: make(map[string]Challenge),
	}
}

func (s *memoryChallengeStore) Put(_ context.Context, ch Challenge, ttl time.Duration) error {
	if ch.ID == "" {
		return fmt.Errorf("%w: empty id", ErrInvalidChallenge)
	}
	if ttl <= 0 {
		return fmt.Errorf("%w: ttl must be positive", ErrInvalidChallenge)
	}

	ch.ExpiresAt = s.clock.Now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[ch.ID] = ch
	return nil
}

func (s *memoryChallengeStore) Get(_ context.Context, id string) (Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.items[id]
	if !ok {
		return Challenge{}, ErrChallengeNotFound
	}
	if s.expired(ch) {
		delete(s.items, id)
		return Challenge{}, ErrChallengeExpired
	}
	return ch, nil
}

func (s *memoryChallengeStore) Consume(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.items[id]
	if !ok {
		return ErrChallengeNotFound
	}
	// Remove first so the entry is single-use regardless of expiry outcome.
	delete(s.items, id)
	if s.expired(ch) {
		return ErrChallengeExpired
	}
	return nil
}

// expired reports whether ch is at or past its expiry relative to the clock.
// The boundary is inclusive: a challenge is expired exactly at ExpiresAt.
func (s *memoryChallengeStore) expired(ch Challenge) bool {
	return !s.clock.Now().Before(ch.ExpiresAt)
}
