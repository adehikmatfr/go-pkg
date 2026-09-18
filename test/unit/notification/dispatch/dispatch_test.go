package dispatch_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/notification/dispatch"
	"github.com/adehikmatfr/go-pkg/v2/notification/email"
	"github.com/adehikmatfr/go-pkg/v2/notification/push"
	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
)

// fakePreferenceResolver returns a fixed preference or a fixed error.
type fakePreferenceResolver struct {
	pref dispatch.ChannelPreference
	err  error
}

func (f fakePreferenceResolver) Resolve(context.Context, string) (dispatch.ChannelPreference, error) {
	return f.pref, f.err
}

// fakeRenderer returns fixed content or a fixed error.
type fakeRenderer struct {
	content dispatch.RenderedContent
	err     error
}

func (f fakeRenderer) Render(context.Context, dispatch.NotificationRequest) (dispatch.RenderedContent, error) {
	return f.content, f.err
}

// fakeDeliveryLog records entries in memory and can be primed as already-delivered.
type fakeDeliveryLog struct {
	mu            sync.Mutex
	entries       []dispatch.DeliveryLogEntry
	delivered     bool
	alreadyErr    error
	recordErr     error
	recordErrOnly dispatch.Channel
}

func (f *fakeDeliveryLog) AlreadyDelivered(context.Context, string, string) (bool, error) {
	return f.delivered, f.alreadyErr
}

func (f *fakeDeliveryLog) Record(_ context.Context, entry dispatch.DeliveryLogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil && (f.recordErrOnly == "" || f.recordErrOnly == entry.Channel) {
		return f.recordErr
	}
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeDeliveryLog) snapshot() []dispatch.DeliveryLogEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dispatch.DeliveryLogEntry, len(f.entries))
	copy(out, f.entries)
	return out
}

// fakeEmailSender records the last message sent, or returns a fixed error.
type fakeEmailSender struct {
	err  error
	last email.EmailMessage
}

func (f *fakeEmailSender) Send(_ context.Context, msg email.EmailMessage) error {
	f.last = msg
	return f.err
}

// fakeSmsSender records the last message sent, or returns a fixed error.
type fakeSmsSender struct {
	err  error
	id   string
	last sms.SmsMessage
}

func (f *fakeSmsSender) Send(_ context.Context, msg sms.SmsMessage) (string, error) {
	f.last = msg
	if f.err != nil {
		return "", f.err
	}
	return f.id, nil
}

// fakePushSender records the last message sent, or returns a fixed error.
type fakePushSender struct {
	err  error
	id   string
	last push.PushMessage
}

func (f *fakePushSender) Send(_ context.Context, msg push.PushMessage) (string, error) {
	f.last = msg
	if f.err != nil {
		return "", f.err
	}
	return f.id, nil
}

// fixedClock always returns the same instant.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func allEnabled() dispatch.ChannelPreference {
	return dispatch.ChannelPreference{Enabled: map[dispatch.Channel]bool{
		dispatch.ChannelEmail: true,
		dispatch.ChannelSMS:   true,
		dispatch.ChannelPush:  true,
	}}
}

func fullContent() dispatch.RenderedContent {
	return dispatch.RenderedContent{
		Email: &dispatch.EmailContent{Subject: "hi", TextBody: "hello"},
		SMS:   &dispatch.SmsContent{Body: "hello"},
		Push:  &dispatch.PushContent{Title: "hi", Body: "hello"},
	}
}

func fullRecipient() dispatch.Recipient {
	return dispatch.Recipient{ID: "user-1", Email: "user@example.com", Phone: "+15558675310", PushToken: "token-1"}
}

func TestNewDispatcher(t *testing.T) {
	prefs := fakePreferenceResolver{}
	render := fakeRenderer{}
	log := &fakeDeliveryLog{}

	tests := []struct {
		name    string
		prefs   dispatch.PreferenceResolver
		render  dispatch.Renderer
		log     dispatch.DeliveryLog
		wantErr error
	}{
		{name: "missing preference resolver", prefs: nil, render: render, log: log, wantErr: dispatch.ErrMissingPreferenceResolver},
		{name: "missing renderer", prefs: prefs, render: nil, log: log, wantErr: dispatch.ErrMissingRenderer},
		{name: "missing delivery log", prefs: prefs, render: render, log: nil, wantErr: dispatch.ErrMissingDeliveryLog},
		{name: "valid", prefs: prefs, render: render, log: log, wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := dispatch.NewDispatcher(dispatch.Senders{}, tt.prefs, tt.render, tt.log)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				if d != nil {
					t.Fatalf("expected nil dispatcher on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d == nil {
				t.Fatalf("expected non-nil dispatcher")
			}
		})
	}
}

func TestDispatch_RequestValidation(t *testing.T) {
	d, err := dispatch.NewDispatcher(dispatch.Senders{}, fakePreferenceResolver{}, fakeRenderer{}, &fakeDeliveryLog{})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	tests := []struct {
		name    string
		req     dispatch.NotificationRequest
		wantErr error
	}{
		{name: "missing idempotency key", req: dispatch.NotificationRequest{Recipient: dispatch.Recipient{ID: "u1"}}, wantErr: dispatch.ErrNoIdempotencyKey},
		{name: "missing recipient id", req: dispatch.NotificationRequest{IdempotencyKey: "k1"}, wantErr: dispatch.ErrNoRecipientID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := d.Dispatch(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDispatch_Suppressed(t *testing.T) {
	log := &fakeDeliveryLog{delivered: true}
	d, err := dispatch.NewDispatcher(dispatch.Senders{}, fakePreferenceResolver{}, fakeRenderer{}, log)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := d.Dispatch(context.Background(), dispatch.NotificationRequest{
		Recipient:      dispatch.Recipient{ID: "u1"},
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Suppressed {
		t.Fatalf("expected Suppressed = true")
	}
	if len(log.snapshot()) != 0 {
		t.Fatalf("expected no log entries for a suppressed dispatch")
	}
}

func TestDispatch_CollaboratorErrors(t *testing.T) {
	req := dispatch.NotificationRequest{Recipient: dispatch.Recipient{ID: "u1"}, IdempotencyKey: "k1"}

	t.Run("already delivered check fails", func(t *testing.T) {
		log := &fakeDeliveryLog{alreadyErr: errors.New("boom")}
		d, _ := dispatch.NewDispatcher(dispatch.Senders{}, fakePreferenceResolver{}, fakeRenderer{}, log)
		if _, err := d.Dispatch(context.Background(), req); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("resolve preferences fails", func(t *testing.T) {
		log := &fakeDeliveryLog{}
		d, _ := dispatch.NewDispatcher(dispatch.Senders{}, fakePreferenceResolver{err: errors.New("boom")}, fakeRenderer{}, log)
		if _, err := d.Dispatch(context.Background(), req); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("render fails", func(t *testing.T) {
		log := &fakeDeliveryLog{}
		d, _ := dispatch.NewDispatcher(dispatch.Senders{}, fakePreferenceResolver{}, fakeRenderer{err: errors.New("boom")}, log)
		if _, err := d.Dispatch(context.Background(), req); err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestDispatch_HappyPath(t *testing.T) {
	emailSender := &fakeEmailSender{}
	smsSender := &fakeSmsSender{id: "sms-1"}
	pushSender := &fakePushSender{id: "push-1"}
	log := &fakeDeliveryLog{}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	d, err := dispatch.NewDispatcher(
		dispatch.Senders{Email: emailSender, SMS: smsSender, Push: pushSender},
		fakePreferenceResolver{pref: allEnabled()},
		fakeRenderer{content: fullContent()},
		log,
		dispatch.WithClock(fixedClock{t: now}),
	)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := d.Dispatch(context.Background(), dispatch.NotificationRequest{
		Recipient:      fullRecipient(),
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Suppressed {
		t.Fatalf("expected Suppressed = false")
	}
	wantChannels := []dispatch.Channel{dispatch.ChannelEmail, dispatch.ChannelSMS, dispatch.ChannelPush}
	if fmt.Sprint(res.Attempted) != fmt.Sprint(wantChannels) {
		t.Fatalf("Attempted = %v, want %v", res.Attempted, wantChannels)
	}
	if fmt.Sprint(res.Delivered) != fmt.Sprint(wantChannels) {
		t.Fatalf("Delivered = %v, want %v", res.Delivered, wantChannels)
	}
	if len(res.Failed) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("expected no Failed/Skipped, got %+v", res)
	}

	entries := log.snapshot()
	if len(entries) != 3 {
		t.Fatalf("got %d log entries, want 3", len(entries))
	}
	for _, e := range entries {
		if e.Status != dispatch.StatusDelivered {
			t.Errorf("channel %s status = %s, want delivered", e.Channel, e.Status)
		}
		if e.RecipientID != "user-1" || e.IdempotencyKey != "key-1" {
			t.Errorf("channel %s: unexpected recipient/idempotency key on entry", e.Channel)
		}
		if !e.AttemptedAt.Equal(now) {
			t.Errorf("channel %s: AttemptedAt = %v, want %v", e.Channel, e.AttemptedAt, now)
		}
	}

	if emailSender.last.Subject != "hi" || emailSender.last.To[0].Email != "user@example.com" {
		t.Errorf("unexpected email sent: %+v", emailSender.last)
	}
	if smsSender.last.To != "+15558675310" {
		t.Errorf("unexpected sms sent: %+v", smsSender.last)
	}
	if pushSender.last.Token != "token-1" {
		t.Errorf("unexpected push sent: %+v", pushSender.last)
	}
}

func TestDispatch_SkipReasons(t *testing.T) {
	tests := []struct {
		name    string
		senders dispatch.Senders
		recip   dispatch.Recipient
		reason  string
	}{
		{
			name:    "no email sender configured",
			senders: dispatch.Senders{},
			recip:   fullRecipient(),
			reason:  "no email sender configured",
		},
		{
			name:    "recipient has no email address",
			senders: dispatch.Senders{Email: &fakeEmailSender{}},
			recip:   dispatch.Recipient{ID: "u1", Phone: "+15558675310", PushToken: "t1"},
			reason:  "recipient has no email address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &fakeDeliveryLog{}
			d, err := dispatch.NewDispatcher(
				tt.senders,
				fakePreferenceResolver{pref: dispatch.ChannelPreference{Enabled: map[dispatch.Channel]bool{dispatch.ChannelEmail: true}}},
				fakeRenderer{content: dispatch.RenderedContent{Email: &dispatch.EmailContent{Subject: "hi", TextBody: "hello"}}},
				log,
			)
			if err != nil {
				t.Fatalf("NewDispatcher: %v", err)
			}

			res, err := d.Dispatch(context.Background(), dispatch.NotificationRequest{
				Recipient:      tt.recip,
				IdempotencyKey: "key-1",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res.Skipped) != 1 || res.Skipped[0] != dispatch.ChannelEmail {
				t.Fatalf("Skipped = %v, want [email]", res.Skipped)
			}
			entries := log.snapshot()
			if len(entries) != 1 || entries[0].Status != dispatch.StatusSkipped || entries[0].Error != tt.reason {
				t.Fatalf("got entries %+v, want a single skipped entry with reason %q", entries, tt.reason)
			}
		})
	}
}

func TestDispatch_ChannelFailure(t *testing.T) {
	sendErr := errors.New("provider down")
	log := &fakeDeliveryLog{}
	d, err := dispatch.NewDispatcher(
		dispatch.Senders{SMS: &fakeSmsSender{err: sendErr}},
		fakePreferenceResolver{pref: dispatch.ChannelPreference{Enabled: map[dispatch.Channel]bool{dispatch.ChannelSMS: true}}},
		fakeRenderer{content: dispatch.RenderedContent{SMS: &dispatch.SmsContent{Body: "hello"}}},
		log,
	)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := d.Dispatch(context.Background(), dispatch.NotificationRequest{
		Recipient:      fullRecipient(),
		IdempotencyKey: "key-1",
	})
	if err == nil {
		t.Fatalf("expected a non-nil error")
	}
	if !errors.Is(err, sendErr) {
		t.Fatalf("got err %v, want it to wrap %v", err, sendErr)
	}
	if !strings.Contains(err.Error(), `channel "sms"`) {
		t.Fatalf("err.Error() = %q, want it to name the failing channel", err.Error())
	}
	if len(res.Failed) != 1 || res.Failed[0] != dispatch.ChannelSMS {
		t.Fatalf("Failed = %v, want [sms]", res.Failed)
	}
	entries := log.snapshot()
	if len(entries) != 1 || entries[0].Status != dispatch.StatusFailed || entries[0].Error != sendErr.Error() {
		t.Fatalf("got entries %+v", entries)
	}
}

func TestDispatch_DisabledOrNoContentChannelsAreNotAttempted(t *testing.T) {
	log := &fakeDeliveryLog{}
	d, err := dispatch.NewDispatcher(
		dispatch.Senders{Email: &fakeEmailSender{}, SMS: &fakeSmsSender{}, Push: &fakePushSender{}},
		fakePreferenceResolver{pref: dispatch.ChannelPreference{Enabled: map[dispatch.Channel]bool{dispatch.ChannelEmail: true}}},
		fakeRenderer{content: dispatch.RenderedContent{}},
		log,
	)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := d.Dispatch(context.Background(), dispatch.NotificationRequest{
		Recipient:      fullRecipient(),
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Attempted) != 0 || len(res.Skipped) != 0 || len(res.Delivered) != 0 || len(res.Failed) != 0 {
		t.Fatalf("expected an empty result, got %+v", res)
	}
	if len(log.snapshot()) != 0 {
		t.Fatalf("expected no log entries")
	}
}

func TestDispatch_RecordFailureIsCollectedButDoesNotAbortFanOut(t *testing.T) {
	log := &fakeDeliveryLog{recordErr: errors.New("db down")}
	d, err := dispatch.NewDispatcher(
		dispatch.Senders{Email: &fakeEmailSender{}, SMS: &fakeSmsSender{id: "sms-1"}},
		fakePreferenceResolver{pref: allEnabled()},
		fakeRenderer{content: dispatch.RenderedContent{
			Email: &dispatch.EmailContent{Subject: "hi", TextBody: "hello"},
			SMS:   &dispatch.SmsContent{Body: "hello"},
		}},
		log,
	)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := d.Dispatch(context.Background(), dispatch.NotificationRequest{
		Recipient:      fullRecipient(),
		IdempotencyKey: "key-1",
	})
	if err == nil {
		t.Fatalf("expected an error joining the record failures")
	}
	if len(res.Delivered) != 2 {
		t.Fatalf("Delivered = %v, want both channels delivered despite the log failure", res.Delivered)
	}
	if len(log.snapshot()) != 0 {
		t.Fatalf("expected no entries to have been recorded")
	}
}
