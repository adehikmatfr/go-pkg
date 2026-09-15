package sms_test

import (
	"context"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/notification/sms"
)

func TestSmsMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     sms.SmsMessage
		wantErr error
	}{
		{name: "valid", msg: sms.SmsMessage{To: "+15558675310", Body: "hi"}},
		{
			name:    "missing recipient",
			msg:     sms.SmsMessage{To: "", Body: "hi"},
			wantErr: sms.ErrMissingRecipient,
		},
		{
			name:    "blank recipient",
			msg:     sms.SmsMessage{To: "   ", Body: "hi"},
			wantErr: sms.ErrMissingRecipient,
		},
		{
			name:    "empty body",
			msg:     sms.SmsMessage{To: "+15558675310", Body: ""},
			wantErr: sms.ErrEmptyBody,
		},
		{
			name:    "blank body",
			msg:     sms.SmsMessage{To: "+15558675310", Body: "   "},
			wantErr: sms.ErrEmptyBody,
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

func TestSendFailure(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("provider down")
		err := sms.SendFailure(cause, "twilio: create message")
		if !errors.Is(err, sms.ErrSendFailed) {
			t.Errorf("SendFailure() = %v, want errors.Is ErrSendFailed", err)
		}
		if !errors.Is(err, cause) {
			t.Errorf("SendFailure() = %v, want errors.Is cause", err)
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := sms.SendFailure(nil, "twilio: no sid returned")
		if !errors.Is(err, sms.ErrSendFailed) {
			t.Errorf("SendFailure() = %v, want errors.Is ErrSendFailed", err)
		}
	})
}

// fakeSender demonstrates the port is small enough to implement outside the
// package, mirroring how a consumer would fake it in their own tests.
type fakeSender struct {
	returnID  string
	returnErr error
	calls     int
}

func (f *fakeSender) Send(_ context.Context, _ sms.SmsMessage) (string, error) {
	f.calls++
	return f.returnID, f.returnErr
}

var _ sms.SmsSender = (*fakeSender)(nil)

func TestFakeSender_satisfiesPort(t *testing.T) {
	f := &fakeSender{returnID: "SM123"}
	id, err := f.Send(context.Background(), sms.SmsMessage{To: "+15558675310", Body: "hi"})
	if err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}
	if id != "SM123" {
		t.Errorf("Send() id = %q, want %q", id, "SM123")
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1", f.calls)
	}
}
