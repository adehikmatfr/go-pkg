package redact

import "io"

// redactorFunc censors a raw log line; the default is RedactBytes.
type redactorFunc func(p []byte) []byte

// writer wraps a downstream io.Writer and censors every line written through
// it.
//
// Content is fail-closed: the raw input never reaches next, and a panicking
// redactor yields the placeholder line instead. Delivery is fail-open: Write
// always reports (len(p), nil), so a redaction fault never surfaces to the
// caller (e.g. zerolog) as a write error. A line always appears, censored
// rather than suppressed.
type writer struct {
	next     io.Writer
	redactor redactorFunc
}

// NewWriter wraps next so that every Write is redacted before it reaches
// next. Use it to wrap os.Stdout/os.Stderr in a logger's bootstrap.
func NewWriter(next io.Writer) io.Writer {
	return &writer{next: next, redactor: RedactBytes}
}

// NewWriterWithRedactor is the injectable form used to exercise the
// fail-closed content path in tests. Production code uses NewWriter.
func NewWriterWithRedactor(next io.Writer, r func(p []byte) []byte) io.Writer {
	if r == nil {
		r = RedactBytes
	}
	return &writer{next: next, redactor: r}
}

func (w *writer) Write(p []byte) (n int, err error) {
	// Content fail-closed: a panic in the redactor cannot leak p, so recover
	// and emit the placeholder line.
	defer func() {
		if rec := recover(); rec != nil {
			_, _ = w.next.Write(placeholderLine) // delivery is fail-open regardless
			n, err = len(p), nil
		}
	}()

	censored := w.redactor(p)
	if _, werr := w.next.Write(censored); werr != nil {
		// The downstream write failed. Delivery is fail-open: report the
		// input as consumed so the caller does not retry with raw bytes.
		return len(p), nil
	}
	return len(p), nil
}
