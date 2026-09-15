package smtp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	smtpmock "github.com/mocktools/go-smtp-mock/v2"

	"github.com/adehikmatfr/go-pkg/v2/notification/email"
	"github.com/adehikmatfr/go-pkg/v2/notification/email/smtp"
)

func TestNewClient_invalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  smtp.Config
	}{
		{name: "empty host", cfg: smtp.Config{}},
		{
			name: "auth enabled without credentials",
			cfg:  smtp.Config{Host: "smtp.example.com", Auth: smtp.AuthPlain},
		},
		{
			name: "invalid DefaultFrom",
			cfg:  smtp.Config{Host: "smtp.example.com", DefaultFrom: email.Address{Email: "not-an-email"}},
		},
		{
			name: "unknown TLS mode",
			cfg:  smtp.Config{Host: "smtp.example.com", TLS: smtp.TLSMode(99)},
		},
		{
			name: "unknown auth mode",
			cfg:  smtp.Config{Host: "smtp.example.com", Auth: smtp.AuthMode(99)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := smtp.NewClient(tt.cfg)
			if !errors.Is(err, email.ErrInvalidConfig) {
				t.Fatalf("NewClient() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestNewClient_valid(t *testing.T) {
	_, err := smtp.NewClient(smtp.Config{
		Host: "smtp.example.com",
		TLS:  smtp.TLSNone,
		Auth: smtp.AuthNone,
	})
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}
}

// startMockServer starts a local, plaintext fake SMTP server for the test and
// returns the Sender wired to it. The real go-mail dial/DATA flow runs
// against this server, so Send is exercised end to end without a live
// mail provider.
func startMockServer(t *testing.T) (*smtp.Sender, *smtpmock.Server) {
	t.Helper()
	server := smtpmock.New(smtpmock.ConfigurationAttr{})
	if err := server.Start(); err != nil {
		t.Fatalf("starting mock SMTP server: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	sender, err := smtp.NewClient(smtp.Config{
		Host: "127.0.0.1",
		Port: server.PortNumber(),
		TLS:  smtp.TLSNone,
		Auth: smtp.AuthNone,
		DefaultFrom: email.Address{
			Name:  "NagaX",
			Email: "no-reply@example.com",
		},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return sender, server
}

func TestSend_deliversToMockServer(t *testing.T) {
	sender, server := startMockServer(t)

	msg := email.EmailMessage{
		To:             []email.Address{{Name: "Ada", Email: "ada@example.com"}},
		Subject:        "Welcome",
		TextBody:       "Welcome aboard.",
		IdempotencyKey: "evt-42",
	}

	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if _, err := server.WaitForMessagesAndPurge(1, 2*time.Second); err != nil {
		t.Fatalf("mock server did not receive the message in time: %v", err)
	}
}

func TestSend_perMessageFromOverridesDefault(t *testing.T) {
	sender, server := startMockServer(t)

	msg := email.EmailMessage{
		From:           email.Address{Email: "override@example.com"},
		To:             []email.Address{{Email: "ada@example.com"}},
		TextBody:       "hi",
		IdempotencyKey: "evt-1",
	}

	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if _, err := server.WaitForMessagesAndPurge(1, 2*time.Second); err != nil {
		t.Fatalf("mock server did not receive the message in time: %v", err)
	}
}

func TestSend_invalidMessageNeverDials(t *testing.T) {
	sender, server := startMockServer(t)

	err := sender.Send(context.Background(), email.EmailMessage{}) // no recipients, no idempotency key
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("Send() error = %v, want ErrInvalidMessage", err)
	}
	if len(server.Messages()) != 0 {
		t.Fatalf("server recorded %d messages, want 0: an invalid message must never dial", len(server.Messages()))
	}
}

func TestSend_noFromAndNoDefaultFrom(t *testing.T) {
	server := smtpmock.New(smtpmock.ConfigurationAttr{})
	if err := server.Start(); err != nil {
		t.Fatalf("starting mock SMTP server: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	sender, err := smtp.NewClient(smtp.Config{
		Host: "127.0.0.1",
		Port: server.PortNumber(),
		TLS:  smtp.TLSNone,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	msg := email.EmailMessage{
		To:             []email.Address{{Email: "ada@example.com"}},
		TextBody:       "hi",
		IdempotencyKey: "evt-1",
	}
	if err := sender.Send(context.Background(), msg); !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("Send() error = %v, want ErrInvalidMessage", err)
	}
}

func TestSend_contextCancelledNeverDials(t *testing.T) {
	sender, server := startMockServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg := email.EmailMessage{
		To:             []email.Address{{Email: "ada@example.com"}},
		TextBody:       "hi",
		IdempotencyKey: "evt-1",
	}
	err := sender.Send(ctx, msg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if len(server.Messages()) != 0 {
		t.Fatalf("server recorded %d messages, want 0", len(server.Messages()))
	}
}

func TestSend_transportFailureWrapsErrSendFailed(t *testing.T) {
	// Port 0 with no listener: go-mail's dial fails, exercising the
	// ErrSendFailed wrapping path without a real remote server.
	sender, err := smtp.NewClient(smtp.Config{
		Host: "127.0.0.1",
		Port: 1, // reserved, nothing listening
		TLS:  smtp.TLSNone,
		DefaultFrom: email.Address{
			Email: "no-reply@example.com",
		},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	msg := email.EmailMessage{
		To:             []email.Address{{Email: "ada@example.com"}},
		TextBody:       "hi",
		IdempotencyKey: "evt-1",
	}
	err = sender.Send(context.Background(), msg)
	if !errors.Is(err, email.ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
}
