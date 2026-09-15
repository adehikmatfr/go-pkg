// Package sms is the SMS notification port: a provider-agnostic SmsSender
// consumers depend on, plus the plain SmsMessage value type. No third-party
// SDK import — a concrete adapter (e.g. notification/sms/twilio) keeps its
// vendor types internal.
package sms

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by every SmsSender implementation. Classify with
// errors.Is, never by matching strings.
var (
	// ErrMissingRecipient indicates the SmsMessage had no destination number.
	ErrMissingRecipient = errors.New("sms: missing recipient")
	// ErrEmptyBody indicates the SmsMessage had no textual body.
	ErrEmptyBody = errors.New("sms: empty body")
	// ErrMissingConfig indicates a provider adapter was constructed with
	// incomplete credentials or configuration.
	ErrMissingConfig = errors.New("sms: missing provider configuration")
	// ErrSendFailed wraps any failure to hand a message to the provider, or
	// a provider response that did not yield a message identifier. Build it
	// with SendFailure so the underlying provider error (when present) is
	// preserved in the chain.
	ErrSendFailed = errors.New("sms: send failed")
)

// SmsMessage is a single outbound text message: a plain value type with no
// provider-specific fields.
type SmsMessage struct {
	// To is the destination phone number, ideally E.164 (e.g.
	// "+15558675310"). This package does not validate the format itself;
	// that is left to the provider.
	To string
	// Body is the message text. A provider may split a long body into
	// multiple segments transparently to the caller.
	Body string
	// IdempotencyKey identifies the logical send so a retried Send need not
	// deliver a duplicate, where the provider supports it. Empty means the
	// send is non-idempotent.
	IdempotencyKey string
}

// Validate reports whether the message is structurally sendable. It does
// not validate the phone-number format; that is left to the provider.
func (m SmsMessage) Validate() error {
	if strings.TrimSpace(m.To) == "" {
		return ErrMissingRecipient
	}
	if strings.TrimSpace(m.Body) == "" {
		return ErrEmptyBody
	}
	return nil
}

// SmsSender is the port for delivering an SMS through some provider.
type SmsSender interface {
	// Send delivers msg and returns the provider's message identifier on
	// success. An implementation validates msg, honours ctx cancellation,
	// and returns an error wrapping ErrSendFailed (via SendFailure) when
	// the provider rejects the send.
	Send(ctx context.Context, msg SmsMessage) (providerMessageID string, err error)
}

// SendFailure builds an error satisfying errors.Is(err, ErrSendFailed) while
// preserving cause in the chain (so errors.Is(err, cause) also holds), so
// every adapter reports a provider failure identically. cause may be nil
// for a well-formed response missing a required field.
func SendFailure(cause error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if cause == nil {
		return fmt.Errorf("%s: %w", msg, ErrSendFailed)
	}
	return fmt.Errorf("%s: %w: %w", msg, ErrSendFailed, cause)
}
