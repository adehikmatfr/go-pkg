package i18n

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"text/template"
)

// ErrMessageKeyNotFound is returned by Translator.Translate when a key is
// missing in both the target and the bundle's base locale.
var ErrMessageKeyNotFound = errors.New("i18n: message key not found in any locale")

// ErrMessageRenderFailed is returned when a key exists but its template
// cannot be rendered with the supplied data.
var ErrMessageRenderFailed = errors.New("i18n: message render failed for supplied data")

// Translator renders the message for key in loc, interpolating data into the
// template via Go's text/template syntax ("{{.Name}}").
//
// Fallback is explicit: a key missing in loc falls back to the bundle's base
// locale, and a key missing everywhere returns ErrMessageKeyNotFound rather
// than an empty string.
//
// Output is not HTML-escaped: an untrusted value in data must be escaped by
// the caller before it enters an HTML body.
type Translator interface {
	Translate(key string, loc Locale, data any) (string, error)
}

// Bundle is a Translator backed by an in-memory message catalog, keyed by
// locale then message key.
type Bundle struct {
	base Locale

	mu       sync.RWMutex
	messages map[Locale]map[string]string
	compiled map[Locale]map[string]*template.Template
}

var _ Translator = (*Bundle)(nil)

// NewBundle returns an empty Bundle whose base (template/authoring) locale
// is base. A key present in base but missing from the requested locale falls
// back to base's string; a key missing from base too is
// ErrMessageKeyNotFound.
func NewBundle(base Locale) *Bundle {
	return &Bundle{
		base:     base,
		messages: make(map[Locale]map[string]string),
		compiled: make(map[Locale]map[string]*template.Template),
	}
}

// AddMessages registers messages (key -> text/template source) for loc,
// merging into any messages already registered for that locale. It returns
// an error if any template fails to parse.
func (b *Bundle) AddMessages(loc Locale, messages map[string]string) error {
	compiled := make(map[string]*template.Template, len(messages))
	for key, src := range messages {
		tmpl, err := template.New(string(loc) + ":" + key).Parse(src)
		if err != nil {
			return fmt.Errorf("i18n: parse message %q for locale %q: %w", key, loc, err)
		}
		compiled[key] = tmpl
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.messages[loc] == nil {
		b.messages[loc] = make(map[string]string, len(messages))
		b.compiled[loc] = make(map[string]*template.Template, len(messages))
	}
	for key, src := range messages {
		b.messages[loc][key] = src
		b.compiled[loc][key] = compiled[key]
	}
	return nil
}

// Translate implements Translator.
func (b *Bundle) Translate(key string, loc Locale, data any) (string, error) {
	b.mu.RLock()
	tmpl, ok := b.compiled[loc][key]
	if !ok {
		tmpl, ok = b.compiled[b.base][key]
	}
	b.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("%w: %q", ErrMessageKeyNotFound, key)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrMessageRenderFailed, key, err)
	}
	return buf.String(), nil
}
