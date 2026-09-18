package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
	"github.com/adehikmatfr/go-pkg/v2/notification/push"
	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
)

// channelError annotates a per-channel failure so collected errors stay
// attributable to their channel after fan-out.
type channelError struct {
	channel Channel
	err     error
}

func (e channelError) Error() string {
	return fmt.Sprintf("dispatch: channel %q: %v", e.channel, e.err)
}

func (e channelError) Unwrap() error { return e.err }

// Dispatcher resolves preferences, renders content and fans a notification
// out to every enabled channel, recording a DeliveryLogEntry per attempt. It
// is safe for concurrent use provided its collaborators are.
type Dispatcher struct {
	senders Senders
	prefs   PreferenceResolver
	render  Renderer
	log     DeliveryLog
	clock   Clock
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithClock overrides the clock used to stamp DeliveryLogEntry.AttemptedAt.
// Defaults to the system clock.
func WithClock(c Clock) Option {
	return func(d *Dispatcher) {
		if c != nil {
			d.clock = c
		}
	}
}

// NewDispatcher constructs a Dispatcher. The preference resolver, renderer
// and delivery log are required; senders are optional per channel.
func NewDispatcher(senders Senders, prefs PreferenceResolver, render Renderer, log DeliveryLog, opts ...Option) (*Dispatcher, error) {
	if prefs == nil {
		return nil, ErrMissingPreferenceResolver
	}
	if render == nil {
		return nil, ErrMissingRenderer
	}
	if log == nil {
		return nil, ErrMissingDeliveryLog
	}
	d := &Dispatcher{
		senders: senders,
		prefs:   prefs,
		render:  render,
		log:     log,
		clock:   systemClock{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

// Result summarizes a Dispatch call.
type Result struct {
	// Suppressed is true when the dispatch was skipped because the
	// (recipient, idempotencyKey) pair was already delivered.
	Suppressed bool
	// Attempted lists the channels that were sent to (delivered or failed).
	Attempted []Channel
	// Delivered lists the channels that succeeded.
	Delivered []Channel
	// Failed lists the channels that returned an error.
	Failed []Channel
	// Skipped lists channels that had rendered content but were unsendable
	// for an addressing or configuration reason. Channels without content
	// are omitted.
	Skipped []Channel
}

// Dispatch delivers the request to every enabled, addressable channel that
// has rendered content. One channel failing does not stop the others:
// per-channel errors are joined into the returned error while Result reports
// what happened on each. An already-delivered (Recipient.ID,
// IdempotencyKey) pair is a no-op returning Result{Suppressed: true} and a
// nil error.
func (d *Dispatcher) Dispatch(ctx context.Context, req NotificationRequest) (Result, error) {
	if req.IdempotencyKey == "" {
		return Result{}, ErrNoIdempotencyKey
	}
	if req.Recipient.ID == "" {
		return Result{}, ErrNoRecipientID
	}

	already, err := d.log.AlreadyDelivered(ctx, req.Recipient.ID, req.IdempotencyKey)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch: idempotency check: %w", err)
	}
	if already {
		return Result{Suppressed: true}, nil
	}

	pref, err := d.prefs.Resolve(ctx, req.Recipient.ID)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch: resolve preferences: %w", err)
	}

	content, err := d.render.Render(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch: render content: %w", err)
	}

	var (
		res   Result
		errs  []error
		plans = d.planChannels(req, pref, content)
	)

	for _, p := range plans {
		// Disabled or content-less channels are not attempted and not recorded.
		if !p.eligible {
			continue
		}
		// Eligible but unsendable: recorded as a skip for the audit trail.
		if p.skip {
			res.Skipped = append(res.Skipped, p.channel)
			d.recordIgnoringError(ctx, req, p.channel, DeliveryLogEntry{
				Status: StatusSkipped,
				Error:  p.skipReason,
			}, &errs)
			continue
		}

		res.Attempted = append(res.Attempted, p.channel)
		providerID, sendErr := p.send(ctx)
		if sendErr != nil {
			res.Failed = append(res.Failed, p.channel)
			errs = append(errs, channelError{channel: p.channel, err: sendErr})
			d.recordIgnoringError(ctx, req, p.channel, DeliveryLogEntry{
				Status: StatusFailed,
				Error:  sendErr.Error(),
			}, &errs)
			continue
		}
		res.Delivered = append(res.Delivered, p.channel)
		d.recordIgnoringError(ctx, req, p.channel, DeliveryLogEntry{
			Status:     StatusDelivered,
			ProviderID: providerID,
		}, &errs)
	}

	return res, errors.Join(errs...)
}

// recordIgnoringError persists an entry with the common fields filled in. A
// persist failure is collected into errs but never aborts the fan-out: the
// audit trail is best-effort alongside delivery.
func (d *Dispatcher) recordIgnoringError(ctx context.Context, req NotificationRequest, ch Channel, entry DeliveryLogEntry, errs *[]error) {
	entry.RecipientID = req.Recipient.ID
	entry.IdempotencyKey = req.IdempotencyKey
	entry.Channel = ch
	entry.AttemptedAt = d.clock.Now()
	if err := d.log.Record(ctx, entry); err != nil {
		*errs = append(*errs, channelError{channel: ch, err: fmt.Errorf("record delivery log: %w", err)})
	}
}

// channelPlan is the resolved decision for one channel.
type channelPlan struct {
	channel    Channel
	eligible   bool // enabled and has rendered content
	skip       bool // eligible but unsendable (no address / no sender)
	skipReason string
	send       func(ctx context.Context) (string, error)
}

// planChannels turns the preference + content + senders into a deterministic,
// per-channel plan. Channel order is fixed (email, sms, push) so the Result
// slices are stable for callers and tests.
func (d *Dispatcher) planChannels(req NotificationRequest, pref ChannelPreference, content RenderedContent) []channelPlan {
	plans := make([]channelPlan, 0, 3)

	{
		p := channelPlan{channel: ChannelEmail}
		if pref.IsEnabled(ChannelEmail) && content.Email != nil {
			p.eligible = true
			switch {
			case d.senders.Email == nil:
				p.skip, p.skipReason = true, "no email sender configured"
			case req.Recipient.Email == "":
				p.skip, p.skipReason = true, "recipient has no email address"
			default:
				c := *content.Email
				addr := req.Recipient.Email
				key := req.IdempotencyKey
				p.send = func(ctx context.Context) (string, error) {
					msg := email.EmailMessage{
						To:             []email.Address{{Email: addr}},
						Subject:        c.Subject,
						HTMLBody:       c.HTMLBody,
						TextBody:       c.TextBody,
						IdempotencyKey: key,
					}
					return "", d.senders.Email.Send(ctx, msg)
				}
			}
		}
		plans = append(plans, p)
	}

	{
		p := channelPlan{channel: ChannelSMS}
		if pref.IsEnabled(ChannelSMS) && content.SMS != nil {
			p.eligible = true
			switch {
			case d.senders.SMS == nil:
				p.skip, p.skipReason = true, "no sms sender configured"
			case req.Recipient.Phone == "":
				p.skip, p.skipReason = true, "recipient has no phone number"
			default:
				c := *content.SMS
				addr := req.Recipient.Phone
				key := req.IdempotencyKey
				p.send = func(ctx context.Context) (string, error) {
					return d.senders.SMS.Send(ctx, sms.SmsMessage{To: addr, Body: c.Body, IdempotencyKey: key})
				}
			}
		}
		plans = append(plans, p)
	}

	{
		p := channelPlan{channel: ChannelPush}
		if pref.IsEnabled(ChannelPush) && content.Push != nil {
			p.eligible = true
			switch {
			case d.senders.Push == nil:
				p.skip, p.skipReason = true, "no push sender configured"
			case req.Recipient.PushToken == "":
				p.skip, p.skipReason = true, "recipient has no push token"
			default:
				c := *content.Push
				token := req.Recipient.PushToken
				key := req.IdempotencyKey
				p.send = func(ctx context.Context) (string, error) {
					return d.senders.Push.Send(ctx, push.PushMessage{
						Token:          token,
						Title:          c.Title,
						Body:           c.Body,
						Data:           c.Data,
						IdempotencyKey: key,
					})
				}
			}
		}
		plans = append(plans, p)
	}

	return plans
}
