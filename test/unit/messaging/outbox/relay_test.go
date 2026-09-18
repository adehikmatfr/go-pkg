package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/messaging/outbox"
)

// fakeStore is a minimal in-memory outbox.Store, hand-rolled so Relay's
// tests don't depend on a specific adapter package.
type fakeStore struct {
	mu             sync.Mutex
	events         map[string]outbox.Event
	claimErr       error
	markPubErr     error
	markFailedErr  error
	claimPendingFn func(limit int, events map[string]outbox.Event) []outbox.Event
}

func newFakeStore() *fakeStore {
	return &fakeStore{events: make(map[string]outbox.Event)}
}

func (s *fakeStore) Add(_ context.Context, _ outbox.Tx, event outbox.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.ID] = event
	return nil
}

func (s *fakeStore) ClaimPending(_ context.Context, limit int) ([]outbox.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if s.claimPendingFn != nil {
		return s.claimPendingFn(limit, s.events), nil
	}
	var out []outbox.Event
	for _, e := range s.events {
		if !e.Dead {
			out = append(out, e)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) MarkPublished(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.markPubErr != nil {
		return s.markPubErr
	}
	for _, id := range ids {
		delete(s.events, id)
	}
	return nil
}

func (s *fakeStore) MarkFailed(_ context.Context, events []outbox.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.markFailedErr != nil {
		return s.markFailedErr
	}
	for _, e := range events {
		s.events[e.ID] = e
	}
	return nil
}

// fakePublisher fails for topics listed in failTopics, and records every
// call otherwise.
type fakePublisher struct {
	mu         sync.Mutex
	failTopics map[string]bool
	published  []string // event topics successfully published
}

func newFakePublisher(failTopics ...string) *fakePublisher {
	set := make(map[string]bool, len(failTopics))
	for _, t := range failTopics {
		set[t] = true
	}
	return &fakePublisher{failTopics: set}
}

func (p *fakePublisher) Publish(_ context.Context, topic string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failTopics[topic] {
		return errors.New("publish failed")
	}
	p.published = append(p.published, topic)
	return nil
}

func (p *fakePublisher) Close() error { return nil }

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestNewRelay(t *testing.T) {
	store := newFakeStore()
	pub := newFakePublisher()

	tests := []struct {
		name    string
		store   outbox.Store
		pub     any
		wantErr error
	}{
		{name: "missing store", store: nil, pub: pub, wantErr: outbox.ErrMissingStore},
		{name: "missing publisher", store: store, pub: nil, wantErr: outbox.ErrMissingPublisher},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := outbox.Config{Store: tt.store}
			if tt.pub != nil {
				cfg.Publisher = tt.pub.(*fakePublisher)
			}
			_, err := outbox.NewRelay(cfg)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}

	r, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: pub})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	if r == nil {
		t.Fatalf("expected a non-nil Relay")
	}
}

func TestDrain_NoEvents(t *testing.T) {
	r, err := outbox.NewRelay(outbox.Config{Store: newFakeStore(), Publisher: newFakePublisher()})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	res, err := r.Drain(context.Background(), 10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res != (outbox.DrainResult{}) {
		t.Fatalf("Drain() with nothing pending = %+v, want the zero value", res)
	}
}

func TestDrain_ClaimError(t *testing.T) {
	store := newFakeStore()
	store.claimErr = errors.New("boom")
	r, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: newFakePublisher()})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	if _, err := r.Drain(context.Background(), 10); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDrain_PublishesAndMarksSuccess(t *testing.T) {
	store := newFakeStore()
	store.events["evt-1"] = outbox.Event{ID: "evt-1", Topic: "orders"}
	store.events["evt-2"] = outbox.Event{ID: "evt-2", Topic: "orders"}

	r, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: newFakePublisher()})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	res, err := r.Drain(context.Background(), 10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res.Claimed != 2 || res.Published != 2 || res.Failed != 0 || res.Dead != 0 {
		t.Fatalf("got %+v, want Claimed=2 Published=2", res)
	}
	if len(store.events) != 0 {
		t.Fatalf("expected published events to be removed, got %d remaining", len(store.events))
	}
}

func TestDrain_FailedEventIsRescheduled(t *testing.T) {
	store := newFakeStore()
	store.events["evt-1"] = outbox.Event{ID: "evt-1", Topic: "orders", RetryCount: 0}

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := outbox.NewRelay(outbox.Config{
		Store:       store,
		Publisher:   newFakePublisher("orders"),
		MaxAttempts: 5,
		Clock:       fixedClock{t: now},
	})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	res, err := r.Drain(context.Background(), 10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res.Failed != 1 || res.Dead != 0 || res.Published != 0 {
		t.Fatalf("got %+v, want Failed=1", res)
	}
	e := store.events["evt-1"]
	if e.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", e.RetryCount)
	}
	if e.Dead {
		t.Errorf("Dead = true, want false (below MaxAttempts)")
	}
	if !e.LastAttemptAt.Equal(now) {
		t.Errorf("LastAttemptAt = %v, want %v", e.LastAttemptAt, now)
	}
	wantNext := now.Add(outbox.DefaultBackoff(1))
	if !e.NextRetryAt.Equal(wantNext) {
		t.Errorf("NextRetryAt = %v, want %v", e.NextRetryAt, wantNext)
	}
}

func TestDrain_EventGoesDeadAtCeiling(t *testing.T) {
	store := newFakeStore()
	store.events["evt-1"] = outbox.Event{ID: "evt-1", Topic: "orders", RetryCount: 2}

	r, err := outbox.NewRelay(outbox.Config{
		Store:       store,
		Publisher:   newFakePublisher("orders"),
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	res, err := r.Drain(context.Background(), 10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res.Dead != 1 || res.Failed != 0 {
		t.Fatalf("got %+v, want Dead=1", res)
	}
	if !store.events["evt-1"].Dead {
		t.Fatalf("expected the event to be marked Dead")
	}
}

func TestDrain_OneFailureDoesNotBlockOthers(t *testing.T) {
	store := newFakeStore()
	store.events["evt-ok"] = outbox.Event{ID: "evt-ok", Topic: "orders"}
	store.events["evt-bad"] = outbox.Event{ID: "evt-bad", Topic: "payments"}

	r, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: newFakePublisher("payments")})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	res, err := r.Drain(context.Background(), 10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res.Published != 1 || res.Failed != 1 {
		t.Fatalf("got %+v, want Published=1 Failed=1", res)
	}
	if _, stillThere := store.events["evt-ok"]; stillThere {
		t.Errorf("evt-ok should have been marked published and removed")
	}
	if _, stillThere := store.events["evt-bad"]; !stillThere {
		t.Errorf("evt-bad should still be present (rescheduled)")
	}
}

func TestDrain_MarkPublishedError(t *testing.T) {
	store := newFakeStore()
	store.events["evt-1"] = outbox.Event{ID: "evt-1", Topic: "orders"}
	store.markPubErr = errors.New("boom")

	r, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: newFakePublisher()})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	if _, err := r.Drain(context.Background(), 10); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDrain_MarkFailedError(t *testing.T) {
	store := newFakeStore()
	store.events["evt-1"] = outbox.Event{ID: "evt-1", Topic: "orders"}
	store.markFailedErr = errors.New("boom")

	r, err := outbox.NewRelay(outbox.Config{Store: store, Publisher: newFakePublisher("orders")})
	if err != nil {
		t.Fatalf("NewRelay: %v", err)
	}
	if _, err := r.Drain(context.Background(), 10); err == nil {
		t.Fatalf("expected an error")
	}
}
