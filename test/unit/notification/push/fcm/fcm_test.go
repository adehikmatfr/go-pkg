package fcm_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"

	"github.com/adehikmatfr/go-pkg/v2/notification/push"
	"github.com/adehikmatfr/go-pkg/v2/notification/push/fcm"
)

func TestBuildMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     push.PushMessage
		check   func(t *testing.T, got *messaging.Message)
		wantErr error
	}{
		{
			name: "token with title and body",
			msg:  push.PushMessage{Token: "tok", Title: "Trade filled", Body: "Your AAPL order executed"},
			check: func(t *testing.T, got *messaging.Message) {
				if got.Token != "tok" || got.Notification == nil || got.Notification.Title != "Trade filled" { //nolint:staticcheck // asserting the deprecated-but-functional Token field BuildMessage sets
					t.Errorf("unexpected message: %+v", got)
				}
			},
		},
		{
			name: "topic data-only, no notification block",
			msg:  push.PushMessage{Topic: "price-alerts", Data: map[string]string{"symbol": "BTC"}},
			check: func(t *testing.T, got *messaging.Message) {
				if got.Topic != "price-alerts" || got.Notification != nil {
					t.Errorf("unexpected message: %+v", got)
				}
				if got.Data["symbol"] != "BTC" {
					t.Errorf("Data = %v, want symbol=BTC", got.Data)
				}
			},
		},
		{
			name: "high priority maps to android/apns values",
			msg:  push.PushMessage{Token: "tok", Body: "b", Priority: push.PriorityHigh},
			check: func(t *testing.T, got *messaging.Message) {
				if got.Android.Priority != "high" {
					t.Errorf("Android.Priority = %q, want high", got.Android.Priority)
				}
				if got.APNS.Headers["apns-priority"] != "10" {
					t.Errorf("apns-priority = %q, want 10", got.APNS.Headers["apns-priority"])
				}
			},
		},
		{
			name: "normal priority maps to android/apns values",
			msg:  push.PushMessage{Token: "tok", Body: "b"},
			check: func(t *testing.T, got *messaging.Message) {
				if got.Android.Priority != "normal" {
					t.Errorf("Android.Priority = %q, want normal", got.Android.Priority)
				}
				if got.APNS.Headers["apns-priority"] != "5" {
					t.Errorf("apns-priority = %q, want 5", got.APNS.Headers["apns-priority"])
				}
			},
		},
		{
			name: "idempotency key becomes collapse keys and data entry",
			msg:  push.PushMessage{Token: "tok", Body: "b", IdempotencyKey: "evt-9f8e"},
			check: func(t *testing.T, got *messaging.Message) {
				if got.Android.CollapseKey != "evt-9f8e" {
					t.Errorf("Android.CollapseKey = %q, want evt-9f8e", got.Android.CollapseKey)
				}
				if got.APNS.Headers["apns-collapse-id"] != "evt-9f8e" {
					t.Errorf("apns-collapse-id = %q, want evt-9f8e", got.APNS.Headers["apns-collapse-id"])
				}
				if got.Data["idempotency_key"] != "evt-9f8e" {
					t.Errorf("Data[idempotency_key] = %q, want evt-9f8e", got.Data["idempotency_key"])
				}
			},
		},
		{name: "no recipient", msg: push.PushMessage{Title: "hi"}, wantErr: push.ErrNoRecipient},
		{name: "ambiguous recipient", msg: push.PushMessage{Token: "t", Topic: "p", Title: "hi"}, wantErr: push.ErrAmbiguousRecipient},
		{name: "empty payload", msg: push.PushMessage{Token: "t"}, wantErr: push.ErrEmptyPayload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fcm.BuildMessage(tt.msg)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("BuildMessage() err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildMessage() unexpected error: %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestBuildMessage_doesNotAliasData(t *testing.T) {
	data := map[string]string{"k": "v"}
	msg := push.PushMessage{Token: "tok", Body: "b", Data: data}

	got, err := fcm.BuildMessage(msg)
	if err != nil {
		t.Fatalf("BuildMessage() error = %v", err)
	}

	data["k"] = "mutated"
	if got.Data["k"] != "v" {
		t.Fatal("BuildMessage aliased the caller's Data map instead of copying it")
	}
}

func TestNewClient_invalidConfig(t *testing.T) {
	_, err := fcm.NewClient(context.Background(), fcm.Config{})
	if !errors.Is(err, push.ErrInvalidConfig) {
		t.Fatalf("NewClient() error = %v, want ErrInvalidConfig", err)
	}
}

// fakeFCMServer stands in for the FCM HTTP v1 endpoint
// (POST /v1/projects/{project}/messages:send). option.WithEndpoint points
// the real Firebase Admin SDK at it, so Send is exercised end to end with
// no live Google credentials.
func fakeFCMServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func newTestSender(t *testing.T, server *httptest.Server) push.PushSender {
	t.Helper()
	sender, err := fcm.NewClient(context.Background(), fcm.Config{ProjectID: "test-project"},
		option.WithEndpoint(server.URL),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return sender
}

func TestSend_returnsProviderMessageID(t *testing.T) {
	server := fakeFCMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"name":"projects/test-project/messages/0:123456"}`)
	})
	sender := newTestSender(t, server)

	id, err := sender.Send(context.Background(), push.PushMessage{Token: "tok", Body: "hi"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "projects/test-project/messages/0:123456" {
		t.Errorf("Send() id = %q, want the provider message name", id)
	}
}

func TestSend_invalidMessageNeverCallsProvider(t *testing.T) {
	server := fakeFCMServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider should not have been called for an invalid message")
	})
	sender := newTestSender(t, server)

	_, err := sender.Send(context.Background(), push.PushMessage{Title: "no recipient"})
	if !errors.Is(err, push.ErrNoRecipient) {
		t.Fatalf("Send() error = %v, want ErrNoRecipient", err)
	}
}

// fcmErrorBody builds an error response body shaped like the FCM HTTP v1
// API's "details" envelope, which is the only field the SDK's classifiers
// (messaging.IsInvalidArgument, etc.) actually inspect — the top-level
// "status" string is not used for classification.
func fcmErrorBody(errorCode, message string) string {
	return fmt.Sprintf(`{"error":{"message":%q,"details":[{`+
		`"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":%q}]}}`,
		message, errorCode)
}

func TestSend_invalidArgumentMapsToInvalidToken(t *testing.T) {
	server := fakeFCMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, fcmErrorBody("INVALID_ARGUMENT", "invalid token"))
	})
	sender := newTestSender(t, server)

	_, err := sender.Send(context.Background(), push.PushMessage{Token: "bad-token", Body: "hi"})
	if !errors.Is(err, push.ErrInvalidToken) {
		t.Fatalf("Send() error = %v, want ErrInvalidToken", err)
	}
}

func TestSend_unregisteredMapsToErrUnregistered(t *testing.T) {
	server := fakeFCMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, fcmErrorBody("UNREGISTERED", "Requested entity was not found."))
	})
	sender := newTestSender(t, server)

	_, err := sender.Send(context.Background(), push.PushMessage{Token: "stale-token", Body: "hi"})
	if !errors.Is(err, push.ErrUnregistered) {
		t.Fatalf("Send() error = %v, want ErrUnregistered", err)
	}
}

func TestSend_internalErrorMapsToUnavailable(t *testing.T) {
	server := fakeFCMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, fcmErrorBody("INTERNAL", "backend error"))
	})
	sender := newTestSender(t, server)

	_, err := sender.Send(context.Background(), push.PushMessage{Token: "tok", Body: "hi"})
	if !errors.Is(err, push.ErrUnavailable) {
		t.Fatalf("Send() error = %v, want ErrUnavailable", err)
	}
}

func TestSend_contextCancelledNeverCallsProvider(t *testing.T) {
	server := fakeFCMServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider should not have been called for an already-cancelled context")
	})
	sender := newTestSender(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sender.Send(ctx, push.PushMessage{Token: "tok", Body: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
}
