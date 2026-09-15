// Package redact censors PII and secret values in log/telemetry output. It is
// a stdlib-only leaf so any other package can depend on it without an import
// cycle.
//
// Fail-closed on content, fail-open on delivery (see Writer): the writer
// never emits raw un-redacted bytes, and a panicking redactor writes a
// placeholder line rather than dropping the log. It over-redacts by
// preference, but leaves 10/13/19-digit timestamps, 32-hex trace ids, and
// UUIDs untouched so logs stay correlatable — which is also why the 16-digit
// national-ID and Luhn-15 card-number passes are `\b`-anchored and miss a
// digit run glued into a longer one. Keeping PII out of the signal at the
// boundary remains the primary control; this package is a backstop.
package redact

import (
	"regexp"
	"strings"
)

// placeholderLine is what Writer emits when the redactor panics: a valid
// one-line JSON object with no input-derived content, so a log still appears
// and says nothing about the raw bytes.
var placeholderLine = []byte(`{"level":"error","redaction":"failed","msg":"log line suppressed"}` + "\n")

// redactedKeys is the case-insensitive set of JSON keys whose string value is
// masked wholesale, whatever the value's shape.
var redactedKeys = []string{
	"password", "passwd", "secret", "token", "authorization",
	"api_key", "apikey", "access_key", "private_key",
	"pin", "otp", "cvv", "cvc",
	"ssn", "dsn",
	"cookie", "set-cookie",
}

// Compiled once at package scope; application order is defined in Redact.
var (
	// (a) Key pass: "<key>":"<value>" for any key in redactedKeys
	// (case-insensitive). Group 1 is the key token, preserved; the value is
	// replaced.
	keyValueRe = regexp.MustCompile(`(?i)"(` + strings.Join(escapeAll(redactedKeys), "|") + `)"\s*:\s*"(?:[^"\\]|\\.)*"`)

	// (b) Value passes, applied in this order.

	// PEM private key block (RSA / EC / generic / OPENSSH). Non-greedy body.
	pemRe = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)

	// DSN credentials: scheme://user:pass@host. Only the credentials are
	// masked; the scheme and host survive for debuggability.
	dsnCredsRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^:/\s]+:[^/\s]*@`)

	// JWT: three base64url segments separated by dots, first segment starts
	// eyJ.
	jwtRe = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

	// Bearer / Basic auth token (the credential after the scheme).
	authRe = regexp.MustCompile(`(?i)(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`)

	// Email address.
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

	// A 16-digit run (avoids common timestamp lengths 10/13/19) — a common
	// national-ID or card-number length.
	id16Re = regexp.MustCompile(`\b\d{16}\b`)

	// Candidate 15-digit run for the Luhn-gated card-number pass.
	pan15Re = regexp.MustCompile(`\b\d{15}\b`)

	// Cheap pre-check: does the line contain a 15-digit run at all? Gates
	// the expensive Luhn ReplaceAllStringFunc pass.
	has15Re = regexp.MustCompile(`\d{15}`)
)

func escapeAll(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = regexp.QuoteMeta(k)
	}
	return out
}

// Redact censors PII and secret values in s. Passes run in a fixed order: the
// JSON key-value pass first, then the value regexes from most-specific (PEM,
// DSN, JWT, auth) to least (email, 16-digit id, Luhn-15 card number).
func Redact(s string) string {
	if s == "" {
		return s
	}

	// (a) Key pass — mask the value of any sensitive JSON key wholesale.
	s = keyValueRe.ReplaceAllStringFunc(s, func(m string) string {
		key := keyValueRe.FindStringSubmatch(m)[1]
		return `"` + key + `":"[REDACTED]"`
	})

	// (b) Value passes in order.
	s = pemRe.ReplaceAllString(s, "[REDACTED:private_key]")
	s = dsnCredsRe.ReplaceAllString(s, "${1}[REDACTED:credentials]@")
	s = jwtRe.ReplaceAllString(s, "[REDACTED:jwt]")
	s = authRe.ReplaceAllString(s, "[REDACTED:auth]")
	s = emailRe.ReplaceAllString(s, "[REDACTED:email]")
	s = id16Re.ReplaceAllString(s, "[REDACTED:id16]")

	// Card-number pass: only Luhn-valid 15-digit runs, gated behind a cheap
	// pre-check. Longer digit runs are left alone; a unix-nano timestamp is
	// 19 digits.
	if has15Re.MatchString(s) {
		s = pan15Re.ReplaceAllStringFunc(s, func(run string) string {
			if luhnValid(run) {
				return "[REDACTED:pan]"
			}
			return run
		})
	}

	return s
}

// RedactBytes is the []byte form of Redact. It allocates a new slice rather
// than mutating p, so a caller holding the original buffer is unaffected.
func RedactBytes(p []byte) []byte {
	if len(p) == 0 {
		return p
	}
	return []byte(Redact(string(p)))
}

// luhnValid reports whether an all-digit string passes the Luhn checksum.
func luhnValid(s string) bool {
	sum := 0
	double := false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
