// Package memory is an in-memory email.EmailSender for tests and local
// development. It records every message and validates exactly as a real
// transport does, so a use case's send path is assertable without standing
// up SMTP.
package memory

import (
	"context"
	"sync"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
)

// Sender is an in-memory email.EmailSender. Its constructor returns the
// concrete type rather than the port interface — unlike a production
// adapter, Sender's entire purpose is exposing extra inspection methods
// (Sent, Count, Last, Reset) a test needs; Sender still satisfies
// email.EmailSender for assigning into code that depends on the port.
// Safe for concurrent use.
type Sender struct {
	mu sync.Mutex
	// sent accumulates successfully recorded messages in send order.
	sent []email.EmailMessage
	// FailWith, when non-nil, is returned after validation passes instead
	// of recording the message, simulating a transport failure.
	FailWith error
}

var _ email.EmailSender = (*Sender)(nil)

// New returns an empty Sender ready for use.
func New() *Sender { return &Sender{} }

// Send validates msg and, unless FailWith is set, records it. It honours
// context cancellation.
func (s *Sender) Send(ctx context.Context, msg email.EmailMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	if s.FailWith != nil {
		return s.FailWith
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	return nil
}

// Sent returns a copy of the messages recorded so far, in send order.
func (s *Sender) Sent() []email.EmailMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]email.EmailMessage, len(s.sent))
	copy(out, s.sent)
	return out
}

// Count returns the number of messages recorded so far.
func (s *Sender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// Last returns the most recently recorded message and true, or the zero
// value and false if nothing has been recorded.
func (s *Sender) Last() (email.EmailMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		return email.EmailMessage{}, false
	}
	return s.sent[len(s.sent)-1], true
}

// Reset clears all recorded messages.
func (s *Sender) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = nil
}
