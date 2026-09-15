package twilio_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
	"github.com/adehikmatfr/go-pkg/v2/notification/sms/twilio"
)

// redirectTransport rewrites every outbound request to target's host before
// delegating to the real transport, so the Twilio SDK's hardcoded
// https://api.twilio.com base URL can be pointed at a local httptest server
// without touching the adapter's exported surface.
type redirectTransport struct {
	target *url.URL
}

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	req.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newTestServer(t *testing.T, handler http.HandlerFunc) (*twilio.Sender, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	sender, err := twilio.NewClient(twilio.Config{
		AccountSID: "ACtest",
		AuthToken:  "authtoken",
		FromNumber: "+15017122661",
		HTTPClient: &http.Client{Transport: redirectTransport{target: target}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return sender, &calls
}

func TestNewClient_missingConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  twilio.Config
	}{
		{name: "empty", cfg: twilio.Config{}},
		{name: "missing account sid", cfg: twilio.Config{AuthToken: "t", FromNumber: "+15017122661"}},
		{name: "missing auth token", cfg: twilio.Config{AccountSID: "ACtest", FromNumber: "+15017122661"}},
		{name: "missing from number", cfg: twilio.Config{AccountSID: "ACtest", AuthToken: "t"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := twilio.NewClient(tt.cfg)
			if !errors.Is(err, sms.ErrMissingConfig) {
				t.Fatalf("NewClient() error = %v, want ErrMissingConfig", err)
			}
		})
	}
}

func TestSend_returnsProviderSid(t *testing.T) {
	sender, calls := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.FormValue("To"); got != "+15558675310" {
			t.Errorf("To = %q, want +15558675310", got)
		}
		if got := r.FormValue("From"); got != "+15017122661" {
			t.Errorf("From = %q, want +15017122661", got)
		}
		if got := r.FormValue("Body"); got != "hello" {
			t.Errorf("Body = %q, want hello", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sid":"SM0123456789"}`)
	})

	id, err := sender.Send(context.Background(), sms.SmsMessage{
		To:             "+15558675310",
		Body:           "hello",
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "SM0123456789" {
		t.Errorf("Send() id = %q, want SM0123456789", id)
	}
	if *calls != 1 {
		t.Errorf("server calls = %d, want 1", *calls)
	}
}

func TestSend_invalidMessageNeverCallsProvider(t *testing.T) {
	sender, calls := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider should not have been called for an invalid message")
	})

	_, err := sender.Send(context.Background(), sms.SmsMessage{Body: "hello"}) // missing To
	if !errors.Is(err, sms.ErrMissingRecipient) {
		t.Fatalf("Send() error = %v, want ErrMissingRecipient", err)
	}
	if *calls != 0 {
		t.Errorf("server calls = %d, want 0", *calls)
	}
}

func TestSend_providerErrorWrapsErrSendFailed(t *testing.T) {
	sender, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"code":21211,"message":"invalid 'To' number"}`)
	})

	_, err := sender.Send(context.Background(), sms.SmsMessage{To: "+1", Body: "hi"})
	if !errors.Is(err, sms.ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
}

func TestSend_missingSidWrapsErrSendFailed(t *testing.T) {
	sender, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"queued"}`) // well-formed but no "sid"
	})

	_, err := sender.Send(context.Background(), sms.SmsMessage{To: "+15558675310", Body: "hi"})
	if !errors.Is(err, sms.ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
}

func TestSend_contextCancelledNeverCallsProvider(t *testing.T) {
	sender, calls := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider should not have been called for an already-cancelled context")
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sender.Send(ctx, sms.SmsMessage{To: "+15558675310", Body: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if *calls != 0 {
		t.Errorf("server calls = %d, want 0", *calls)
	}
}
