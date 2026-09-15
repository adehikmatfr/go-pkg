// Package push is the push-notification port: a provider-agnostic PushSender
// consumers depend on, plus the domain types describing a single push. No
// third-party SDK import — a concrete adapter (e.g. notification/push/fcm)
// keeps its vendor types internal.
package push

import (
	"context"
	"errors"
)

// Priority expresses how urgently a push should be delivered. An adapter
// maps it to the target platform's native priority (e.g. FCM android
// "high"/"normal", APNS apns-priority "10"/"5").
type Priority uint8

const (
	// PriorityNormal is the default, battery-friendly delivery priority.
	PriorityNormal Priority = iota
	// PriorityHigh requests immediate delivery; use sparingly.
	PriorityHigh
)

// String returns the canonical lowercase name of the priority.
func (p Priority) String() string {
	if p == PriorityHigh {
		return "high"
	}
	return "normal"
}

// PushMessage is the provider-agnostic description of a single push.
// Exactly one of Token or Topic must be set: Token targets one device
// registration, Topic fans out to every subscriber of a named topic.
type PushMessage struct {
	// Token is the device registration token to deliver to. Mutually
	// exclusive with Topic.
	Token string
	// Topic is the provider topic name to broadcast to. Mutually exclusive
	// with Token.
	Topic string
	// Title is the notification title. May be empty for a data-only
	// (silent) message.
	Title string
	// Body is the notification body text. May be empty for a data-only
	// message.
	Body string
	// Data is an optional key/value payload delivered to the app. Do not
	// place secrets, tokens, or PII here.
	Data map[string]string
	// Priority controls delivery urgency. The zero value is PriorityNormal.
	Priority Priority
	// IdempotencyKey deduplicates retries of the same logical send, where
	// the provider supports a collapse/dedup key. Set it for any retryable
	// send.
	IdempotencyKey string
}

// Validate checks the invariants every PushSender relies on: exactly one
// recipient (Token xor Topic), and at least one of a notification
// (Title/Body) or a Data payload.
func (m PushMessage) Validate() error {
	hasToken := m.Token != ""
	hasTopic := m.Topic != ""
	switch {
	case hasToken && hasTopic:
		return ErrAmbiguousRecipient
	case !hasToken && !hasTopic:
		return ErrNoRecipient
	}
	if m.Title == "" && m.Body == "" && len(m.Data) == 0 {
		return ErrEmptyPayload
	}
	return nil
}

// Sentinel errors returned by PushSender implementations and PushMessage
// validation. Match with errors.Is. ErrInvalidToken and ErrUnregistered are
// permanent (do not retry the same token); ErrUnavailable is transient.
var (
	// ErrNoRecipient indicates neither Token nor Topic was set.
	ErrNoRecipient = errors.New("push: message has no recipient (set Token or Topic)")
	// ErrAmbiguousRecipient indicates both Token and Topic were set.
	ErrAmbiguousRecipient = errors.New("push: message has both Token and Topic set")
	// ErrEmptyPayload indicates the message carries neither notification nor data.
	ErrEmptyPayload = errors.New("push: message has empty payload (set Title/Body or Data)")
	// ErrInvalidToken indicates the provider rejected the token as malformed.
	ErrInvalidToken = errors.New("push: invalid registration token")
	// ErrUnregistered indicates the token is no longer valid (app
	// uninstalled or token rotated); the caller should stop sending to it.
	ErrUnregistered = errors.New("push: registration token is no longer registered")
	// ErrUnavailable indicates a transient provider/server error; safe to
	// retry with backoff.
	ErrUnavailable = errors.New("push: provider temporarily unavailable")
	// ErrInvalidConfig indicates a provider adapter was constructed with
	// incomplete configuration.
	ErrInvalidConfig = errors.New("push: invalid provider configuration")
)

// PushSender is the port for delivering a push through some provider.
type PushSender interface {
	// Send delivers msg and returns the provider's message ID on success.
	// Implementations validate msg and return one of the sentinel errors
	// above (wrapped) on failure.
	Send(ctx context.Context, msg PushMessage) (providerMessageID string, err error)
}
