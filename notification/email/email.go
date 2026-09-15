// Package email is the email notification port: a vendor-agnostic
// EmailSender consumers depend on, plus the domain types it operates on.
// Concrete transports (an SMTP adapter, an in-memory fake for tests) keep
// their own SDK types internal — a caller never sees anything but this
// package's types and the EmailSender interface. No third-party import.
package email

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned (wrapped) by every EmailSender implementation.
// Match them with errors.Is.
var (
	// ErrInvalidMessage indicates the EmailMessage failed validation before
	// any transport was contacted.
	ErrInvalidMessage = errors.New("email: invalid message")
	// ErrSendFailed indicates the underlying transport rejected or failed to
	// deliver the message.
	ErrSendFailed = errors.New("email: send failed")
	// ErrInvalidConfig indicates a transport could not be constructed from
	// the supplied configuration.
	ErrInvalidConfig = errors.New("email: invalid config")
)

// Address is a single email recipient or sender. Name is optional; when set
// it produces an RFC 5322 "Name <addr>" form.
type Address struct {
	// Name is the optional display name.
	Name string
	// Email is the addr-spec (e.g. "user@example.com"). Required.
	Email string
}

// String renders the address in RFC 5322 form: "Name <email>" when a name is
// present, otherwise just the bare address.
func (a Address) String() string {
	if a.Name == "" {
		return a.Email
	}
	return fmt.Sprintf("%s <%s>", a.Name, a.Email)
}

// Validate reports whether the address carries a plausible addr-spec. It is
// a cheap structural check, not full RFC 5322 validation; a transport should
// let its underlying library perform authoritative parsing on top of this.
func (a Address) Validate() error {
	if strings.TrimSpace(a.Email) == "" {
		return fmt.Errorf("%w: empty recipient address", ErrInvalidMessage)
	}
	if !strings.Contains(a.Email, "@") {
		return fmt.Errorf("%w: address %q missing '@'", ErrInvalidMessage, a.Email)
	}
	return nil
}

// reservedHeaders are managed via EmailMessage's structured fields and must
// not be overridden through its Headers map, so a caller cannot smuggle a
// forged To/From/Subject/Message-ID past the fields meant to carry them.
var reservedHeaders = map[string]struct{}{
	"to":         {},
	"from":       {},
	"subject":    {},
	"message-id": {},
}

// IsReservedHeader reports whether name (case-insensitive) is one of the
// headers EmailMessage's structured fields already own. Every transport
// adapter should skip a header for which this returns true rather than
// applying it, so header-smuggling protection is identical across backends.
func IsReservedHeader(name string) bool {
	_, reserved := reservedHeaders[strings.ToLower(name)]
	return reserved
}

// EmailMessage is the transport-independent representation of an email to
// send. At least one of HTMLBody or TextBody must be non-empty; when both
// are set a transport should emit a multipart/alternative message.
type EmailMessage struct {
	// From is the sender. If zero, the transport's configured default
	// sender is used.
	From Address
	// To is the list of primary recipients. At least one is required.
	To []Address
	// Subject is the message subject line.
	Subject string
	// HTMLBody is the text/html part. Optional if TextBody is set.
	HTMLBody string
	// TextBody is the text/plain part. Optional if HTMLBody is set.
	TextBody string
	// IdempotencyKey uniquely identifies this logical message. A transport
	// should use it as the Message-ID so retries are de-duplicable by the
	// receiving infrastructure. Required.
	IdempotencyKey string
	// Headers carries additional RFC 5322 headers to set on the message
	// (e.g. "X-Entity-Ref-ID"). A header for which IsReservedHeader is true
	// is ignored in favour of the structured fields above.
	Headers map[string]string
}

// Validate checks the message for the minimum requirements every transport
// relies on. It returns an error wrapping ErrInvalidMessage on failure.
func (m EmailMessage) Validate() error {
	if strings.TrimSpace(m.IdempotencyKey) == "" {
		return fmt.Errorf("%w: missing idempotency key", ErrInvalidMessage)
	}
	if len(m.To) == 0 {
		return fmt.Errorf("%w: no recipients", ErrInvalidMessage)
	}
	for _, to := range m.To {
		if err := to.Validate(); err != nil {
			return err
		}
	}
	if m.From.Email != "" {
		if err := m.From.Validate(); err != nil {
			return fmt.Errorf("%w (from)", err)
		}
	}
	if strings.TrimSpace(m.HTMLBody) == "" && strings.TrimSpace(m.TextBody) == "" {
		return fmt.Errorf("%w: both HTML and text bodies are empty", ErrInvalidMessage)
	}
	return nil
}

// EmailSender is the port for sending email. It is the only abstraction
// consumers should depend on; concrete adapters (SMTP, an in-memory fake)
// implement it.
type EmailSender interface {
	// Send delivers msg. It returns a non-nil error wrapping
	// ErrInvalidMessage for validation failures or ErrSendFailed for
	// transport failures, and must honour ctx cancellation.
	Send(ctx context.Context, msg EmailMessage) error
}
