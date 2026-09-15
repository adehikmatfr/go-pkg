package email_test

import (
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
)

func TestAddress_String(t *testing.T) {
	tests := []struct {
		name string
		addr email.Address
		want string
	}{
		{name: "with name", addr: email.Address{Name: "Ada", Email: "ada@example.com"}, want: "Ada <ada@example.com>"},
		{name: "without name", addr: email.Address{Email: "ada@example.com"}, want: "ada@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addr.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddress_Validate(t *testing.T) {
	tests := []struct {
		name    string
		addr    email.Address
		wantErr bool
	}{
		{name: "valid", addr: email.Address{Email: "ada@example.com"}},
		{name: "empty", addr: email.Address{Email: ""}, wantErr: true},
		{name: "missing at sign", addr: email.Address{Email: "not-an-email"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.addr.Validate()
			if tt.wantErr && !errors.Is(err, email.ErrInvalidMessage) {
				t.Fatalf("Validate() = %v, want ErrInvalidMessage", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestEmailMessage_Validate(t *testing.T) {
	valid := func() email.EmailMessage {
		return email.EmailMessage{
			To:             []email.Address{{Email: "ada@example.com"}},
			TextBody:       "hello",
			IdempotencyKey: "evt-1",
		}
	}

	tests := []struct {
		name    string
		mutate  func(m email.EmailMessage) email.EmailMessage
		wantErr bool
	}{
		{name: "valid message", mutate: func(m email.EmailMessage) email.EmailMessage { return m }},
		{
			name:    "missing idempotency key",
			mutate:  func(m email.EmailMessage) email.EmailMessage { m.IdempotencyKey = ""; return m },
			wantErr: true,
		},
		{
			name:    "no recipients",
			mutate:  func(m email.EmailMessage) email.EmailMessage { m.To = nil; return m },
			wantErr: true,
		},
		{
			name: "invalid recipient",
			mutate: func(m email.EmailMessage) email.EmailMessage {
				m.To = []email.Address{{Email: "not-an-email"}}
				return m
			},
			wantErr: true,
		},
		{
			name: "invalid from",
			mutate: func(m email.EmailMessage) email.EmailMessage {
				m.From = email.Address{Email: "not-an-email"}
				return m
			},
			wantErr: true,
		},
		{
			name: "both bodies empty",
			mutate: func(m email.EmailMessage) email.EmailMessage {
				m.TextBody = ""
				m.HTMLBody = ""
				return m
			},
			wantErr: true,
		},
		{
			name: "html body only is fine",
			mutate: func(m email.EmailMessage) email.EmailMessage {
				m.TextBody = ""
				m.HTMLBody = "<p>hi</p>"
				return m
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mutate(valid()).Validate()
			if tt.wantErr && !errors.Is(err, email.ErrInvalidMessage) {
				t.Fatalf("Validate() = %v, want ErrInvalidMessage", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestIsReservedHeader(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "To", want: true},
		{name: "from", want: true},
		{name: "SUBJECT", want: true},
		{name: "Message-ID", want: true},
		{name: "X-Entity-Ref-ID", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := email.IsReservedHeader(tt.name); got != tt.want {
				t.Errorf("IsReservedHeader(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
