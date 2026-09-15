// Package memory is an in-memory push.PushSender for tests and local
// development. It records every accepted message and lets a test script the
// response, with no network or vendor SDK.
package memory

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/adehikmatfr/go-pkg/v2/notification/push"
)

// Sender is an in-memory push.PushSender. The zero value is ready to use and
// accepts every valid message, returning a generated provider message ID.
// Its constructor returns the concrete type rather than the port interface
// — like notification/email/memory, its whole purpose is exposing extra
// inspection methods (Sent, Len, Reset) a test needs. Safe for concurrent
// use.
type Sender struct {
	// IDFunc generates the provider message ID returned on success. When
	// nil a random UUID is used.
	IDFunc func(push.PushMessage) string
	// Err, when non-nil, is returned by Send after validation, simulating a
	// provider failure. The message is still recorded before Err is
	// returned, mirroring a real provider that accepts then later fails.
	Err error

	mu   sync.Mutex
	sent []push.PushMessage
}

var _ push.PushSender = (*Sender)(nil)

// New returns an empty Sender ready for use.
func New() *Sender { return &Sender{} }

// Send validates msg, records it, and returns either the scripted Err or a
// generated provider message ID. It honours context cancellation.
func (s *Sender) Send(ctx context.Context, msg push.PushMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := msg.Validate(); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.sent = append(s.sent, msg)
	s.mu.Unlock()

	if s.Err != nil {
		return "", s.Err
	}
	if s.IDFunc != nil {
		return s.IDFunc(msg), nil
	}
	return uuid.NewString(), nil
}

// Sent returns a copy of the messages accepted so far, in order.
func (s *Sender) Sent() []push.PushMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]push.PushMessage, len(s.sent))
	copy(out, s.sent)
	return out
}

// Len reports how many messages have been accepted.
func (s *Sender) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// Reset clears the recorded messages.
func (s *Sender) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = nil
}
