package redact_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/observability/redact"
)

func TestRedactSensitiveKey(t *testing.T) {
	in := `{"user":"alice","password":"hunter2"}`
	got := redact.Redact(in)
	if strings.Contains(got, "hunter2") {
		t.Errorf("Redact() = %q, still contains the plaintext password", got)
	}
	if !strings.Contains(got, `"password":"[REDACTED]"`) {
		t.Errorf("Redact() = %q, want the password key preserved with a redacted value", got)
	}
	if !strings.Contains(got, "alice") {
		t.Errorf("Redact() = %q, should not touch unrelated fields", got)
	}
}

func TestRedactPEMPrivateKey(t *testing.T) {
	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJB\n-----END RSA PRIVATE KEY-----"
	got := redact.Redact(in)
	if strings.Contains(got, "MIIBOgIBAAJB") {
		t.Errorf("Redact() = %q, still contains key material", got)
	}
}

func TestRedactDSNCredentials(t *testing.T) {
	in := "connecting to postgres://user:s3cr3t@db.internal:5432/app"
	got := redact.Redact(in)
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("Redact() = %q, still contains the password", got)
	}
	if !strings.Contains(got, "postgres://") || !strings.Contains(got, "db.internal") {
		t.Errorf("Redact() = %q, should preserve scheme and host", got)
	}
}

func TestRedactJWT(t *testing.T) {
	in := "token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	got := redact.Redact(in)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("Redact() = %q, still contains the JWT", got)
	}
}

func TestRedactEmail(t *testing.T) {
	got := redact.Redact("contact user@example.com for help")
	if strings.Contains(got, "user@example.com") {
		t.Errorf("Redact() = %q, still contains the email", got)
	}
}

func TestRedactLuhnValidCardNumber(t *testing.T) {
	// 379354508162306 is a well-known Luhn-valid American Express test number.
	got := redact.Redact("card 379354508162306 charged")
	if strings.Contains(got, "379354508162306") {
		t.Errorf("Redact() = %q, still contains the card number", got)
	}
}

func TestRedactLuhnInvalidNumberUntouched(t *testing.T) {
	in := "reference 123456789012345 recorded"
	got := redact.Redact(in)
	if !strings.Contains(got, "123456789012345") {
		t.Errorf("Redact() = %q, a Luhn-invalid 15-digit run should be left alone", got)
	}
}

func TestRedactPreservesTimestampsAndEmpty(t *testing.T) {
	if got := redact.Redact(""); got != "" {
		t.Errorf("Redact(\"\") = %q, want empty", got)
	}
	// 19-digit unix-nano timestamp must survive.
	ts := "1732000000000000000"
	if got := redact.Redact(ts); got != ts {
		t.Errorf("Redact(%q) = %q, want unchanged", ts, got)
	}
}

func TestRedactBytes(t *testing.T) {
	if got := redact.RedactBytes(nil); got != nil {
		t.Errorf("RedactBytes(nil) = %v, want nil", got)
	}
	got := redact.RedactBytes([]byte(`{"token":"abc123"}`))
	if strings.Contains(string(got), "abc123") {
		t.Errorf("RedactBytes() = %q, still contains the token", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

type recordingWriter struct{ lines [][]byte }

func (w *recordingWriter) Write(p []byte) (int, error) {
	cp := append([]byte(nil), p...)
	w.lines = append(w.lines, cp)
	return len(p), nil
}

func TestWriterRedactsBeforeWriting(t *testing.T) {
	rec := &recordingWriter{}
	w := redact.NewWriter(rec)

	n, err := w.Write([]byte(`{"password":"secret"}`))
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if n != len(`{"password":"secret"}`) {
		t.Errorf("Write() n = %d, want %d", n, len(`{"password":"secret"}`))
	}
	if len(rec.lines) != 1 || strings.Contains(string(rec.lines[0]), "secret") {
		t.Errorf("downstream received %q, still contains the secret", rec.lines)
	}
}

func TestWriterFailClosedOnPanic(t *testing.T) {
	rec := &recordingWriter{}
	w := redact.NewWriterWithRedactor(rec, func([]byte) []byte {
		panic("redactor exploded")
	})

	n, err := w.Write([]byte("sensitive payload"))
	if err != nil {
		t.Fatalf("Write() error: %v, want nil (fail-open on delivery)", err)
	}
	if n != len("sensitive payload") {
		t.Errorf("Write() n = %d, want %d", n, len("sensitive payload"))
	}
	if len(rec.lines) != 1 || strings.Contains(string(rec.lines[0]), "sensitive payload") {
		t.Errorf("downstream received %q, want the placeholder line only", rec.lines)
	}
}

func TestWriterFailOpenOnDownstreamError(t *testing.T) {
	w := redact.NewWriter(failingWriter{})
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error: %v, want nil (fail-open on delivery)", err)
	}
	if n != len("hello") {
		t.Errorf("Write() n = %d, want %d", n, len("hello"))
	}
}

func TestNewWriterWithRedactorNilFallsBackToDefault(t *testing.T) {
	rec := &recordingWriter{}
	w := redact.NewWriterWithRedactor(rec, nil)
	if _, err := w.Write([]byte(`{"password":"secret"}`)); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if strings.Contains(string(rec.lines[0]), "secret") {
		t.Errorf("downstream received %q, still contains the secret", rec.lines)
	}
}
