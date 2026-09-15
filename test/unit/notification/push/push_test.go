package push_test

import (
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/notification/push"
)

func TestPriority_String(t *testing.T) {
	tests := []struct {
		name string
		p    push.Priority
		want string
	}{
		{name: "normal", p: push.PriorityNormal, want: "normal"},
		{name: "high", p: push.PriorityHigh, want: "high"},
		{name: "unknown falls back to normal", p: push.Priority(99), want: "normal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPushMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     push.PushMessage
		wantErr error
	}{
		{name: "valid with token and title", msg: push.PushMessage{Token: "tok", Title: "hi"}},
		{name: "valid with topic and data", msg: push.PushMessage{Topic: "price-alerts", Data: map[string]string{"px": "1"}}},
		{name: "valid data-only with token", msg: push.PushMessage{Token: "tok", Data: map[string]string{"k": "v"}}},
		{
			name:    "no recipient",
			msg:     push.PushMessage{Title: "hi"},
			wantErr: push.ErrNoRecipient,
		},
		{
			name:    "ambiguous recipient",
			msg:     push.PushMessage{Token: "tok", Topic: "topic", Title: "hi"},
			wantErr: push.ErrAmbiguousRecipient,
		},
		{
			name:    "empty payload",
			msg:     push.PushMessage{Token: "tok"},
			wantErr: push.ErrEmptyPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
