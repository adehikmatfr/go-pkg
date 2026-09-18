// Package dispatch fans a single logical notification out to every channel a
// recipient has enabled, recording a delivery-log entry per attempt. It
// orchestrates the sibling notification/email, notification/sms and
// notification/push ports (same-category imports) rather than declaring its
// own sender interfaces, so a caller wires the exact adapters it already has.
package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
	"github.com/adehikmatfr/go-pkg/v2/notification/push"
	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
)

// Sentinel errors returned by NewDispatcher and Dispatch.
var (
	// ErrMissingPreferenceResolver is returned when NewDispatcher is called
	// with a nil PreferenceResolver.
	ErrMissingPreferenceResolver = errors.New("dispatch: preference resolver is required")
	// ErrMissingRenderer is returned when NewDispatcher is called with a nil
	// Renderer.
	ErrMissingRenderer = errors.New("dispatch: renderer is required")
	// ErrMissingDeliveryLog is returned when NewDispatcher is called with a
	// nil DeliveryLog.
	ErrMissingDeliveryLog = errors.New("dispatch: delivery log is required")
	// ErrNoIdempotencyKey is returned when a request has an empty
	// IdempotencyKey. Idempotency is mandatory for a retryable dispatch.
	ErrNoIdempotencyKey = errors.New("dispatch: idempotency key is required")
	// ErrNoRecipientID is returned when a request has an empty Recipient.ID.
	ErrNoRecipientID = errors.New("dispatch: recipient id is required")
)

// Channel identifies a delivery channel. It is the stable key used in a
// recipient's ChannelPreference and in DeliveryLogEntry.
type Channel string

// The supported delivery channels.
const (
	// ChannelEmail is e-mail delivery.
	ChannelEmail Channel = "email"
	// ChannelSMS is SMS/text-message delivery.
	ChannelSMS Channel = "sms"
	// ChannelPush is mobile/web push notification delivery.
	ChannelPush Channel = "push"
)

// Recipient identifies and addresses the target of a notification. The
// address fields are optional per channel; a channel is only attempted when
// it is both enabled in the recipient's ChannelPreference and has a usable
// address here.
type Recipient struct {
	// ID is the stable recipient identifier (e.g. customer ID). It forms half
	// of the idempotency key and is recorded on every DeliveryLogEntry.
	ID string
	// Email is the recipient's e-mail address, used by the email channel.
	Email string
	// Phone is the recipient's phone number, used by the SMS channel.
	Phone string
	// PushToken is the recipient's device/registration token, used by the
	// push channel.
	PushToken string
}

// ChannelPreference captures which channels a recipient has enabled. A
// channel absent from the map is treated as disabled (opt-in semantics).
type ChannelPreference struct {
	// Enabled maps a Channel to whether the recipient wants it. Absent == off.
	Enabled map[Channel]bool
}

// IsEnabled reports whether the given channel is enabled. A nil or missing
// entry is treated as disabled.
func (p ChannelPreference) IsEnabled(c Channel) bool {
	return p.Enabled[c]
}

// NotificationRequest is a single logical notification to deliver. The
// dispatcher resolves the recipient's preferences, renders per-channel
// content from TemplateKey + Data, and fans out to each enabled channel.
type NotificationRequest struct {
	// Recipient is the target of the notification.
	Recipient Recipient
	// TemplateKey selects the template/content set to render per channel.
	TemplateKey string
	// Data carries the template variables. It is never mutated.
	Data map[string]any
	// IdempotencyKey de-duplicates retries: together with Recipient.ID it
	// uniquely identifies this notification. Re-dispatching the same pair is
	// suppressed. Required.
	IdempotencyKey string
}

// DeliveryStatus is the outcome of a single channel delivery attempt.
type DeliveryStatus string

// The possible delivery outcomes.
const (
	// StatusDelivered means the channel accepted the message.
	StatusDelivered DeliveryStatus = "delivered"
	// StatusFailed means the channel returned an error.
	StatusFailed DeliveryStatus = "failed"
	// StatusSkipped means the channel was not attempted (disabled, missing
	// address, or no rendered content).
	StatusSkipped DeliveryStatus = "skipped"
)

// DeliveryLogEntry is the immutable, append-only record of one channel
// delivery attempt for a notification. The DeliveryLog port persists these.
type DeliveryLogEntry struct {
	// RecipientID is the recipient this attempt targeted.
	RecipientID string
	// IdempotencyKey is the request's idempotency key.
	IdempotencyKey string
	// Channel is the channel that was attempted.
	Channel Channel
	// Status is the outcome of the attempt.
	Status DeliveryStatus
	// ProviderID is the upstream provider's message identifier, when
	// delivered and the channel's sender returns one (email does not).
	ProviderID string
	// Error is the failure reason, when Status is StatusFailed or
	// StatusSkipped. It is a string (not an error) so the entry is a plain,
	// persistable value.
	Error string
	// AttemptedAt is when the attempt was made (UTC).
	AttemptedAt time.Time
}

// EmailContent is the rendered, address-independent e-mail content. At least
// one of HTMLBody or TextBody must be set, per email.EmailMessage.
type EmailContent struct {
	// Subject is the rendered subject line.
	Subject string
	// HTMLBody is the rendered text/html part.
	HTMLBody string
	// TextBody is the rendered text/plain part.
	TextBody string
}

// SmsContent is the rendered SMS content.
type SmsContent struct {
	// Body is the rendered message text.
	Body string
}

// PushContent is the rendered push content.
type PushContent struct {
	// Title is the rendered title.
	Title string
	// Body is the rendered body.
	Body string
	// Data carries optional push key/value metadata.
	Data map[string]string
}

// RenderedContent is the per-channel content the Renderer produces from a
// request's TemplateKey and Data. A nil field skips that channel even when
// the recipient has it enabled.
type RenderedContent struct {
	// Email is the rendered e-mail content, or nil if the template has none.
	Email *EmailContent
	// SMS is the rendered SMS content, or nil.
	SMS *SmsContent
	// Push is the rendered push content, or nil.
	Push *PushContent
}

// Renderer selects and renders the per-channel content for a request from its
// TemplateKey and Data. Implementations must not mutate the request. A
// RenderedContent with every field nil causes every channel to be skipped.
type Renderer interface {
	Render(ctx context.Context, req NotificationRequest) (RenderedContent, error)
}

// PreferenceResolver resolves a recipient's ChannelPreference. Implementations
// typically read from a customer service or a preferences store.
type PreferenceResolver interface {
	Resolve(ctx context.Context, recipientID string) (ChannelPreference, error)
}

// DeliveryLog is the port for the immutable delivery audit trail and the
// idempotency source of truth.
type DeliveryLog interface {
	// AlreadyDelivered reports whether the (recipientID, idempotencyKey) pair
	// has already had at least one successful delivery recorded. The
	// dispatcher uses this to suppress duplicate dispatches.
	AlreadyDelivered(ctx context.Context, recipientID, idempotencyKey string) (bool, error)
	// Record appends a single attempt entry. It must be append-only.
	Record(ctx context.Context, entry DeliveryLogEntry) error
}

// Senders bundles the per-channel sender ports, so a caller wires only the
// channels it supports. A nil sender means the channel is unconfigured and is
// skipped even when enabled.
type Senders struct {
	// Email delivers e-mail, or nil if email is not configured.
	Email email.EmailSender
	// SMS delivers SMS, or nil if SMS is not configured.
	SMS sms.SmsSender
	// Push delivers push notifications, or nil if push is not configured.
	Push push.PushSender
}

// Clock returns the current time, so tests can stamp DeliveryLogEntry
// deterministically. Package-local, matching the security/otp precedent,
// rather than a shared clock dependency for the one package that needs it.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
