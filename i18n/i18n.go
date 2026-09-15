// Package i18n provides locale resolution and a small message bundle for
// translating a key into a locale-specific string, with named-placeholder
// interpolation via the standard library's text/template.
//
// This is a leaner package than a full ICU MessageFormat implementation: it
// does not handle plural/select rules, date/number/money formatting, or
// catalog file loading — only key-to-template lookup with fallback and
// placeholder substitution. Reach for a dedicated ICU library directly if a
// consumer needs plural rules.
package i18n

import (
	"errors"
	"strings"
)

// ErrUnsupportedLocale is returned by ParseLocale for an empty or malformed
// BCP-47 tag.
var ErrUnsupportedLocale = errors.New("i18n: unsupported locale")

// Locale is a canonical BCP-47 primary language subtag (region/script
// variants fold to the base language: "id-ID" -> "id").
type Locale string

// String returns the canonical tag.
func (l Locale) String() string { return string(l) }

// ParseLocale canonicalizes tag: it trims/case-folds and folds a
// region/script variant to the primary language subtag ("en-US" -> "en").
// It returns ErrUnsupportedLocale for anything that isn't a 2-3 letter
// language subtag.
func ParseLocale(tag string) (Locale, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", ErrUnsupportedLocale
	}
	// BCP-47 uses '-'; some sources use '_' (id_ID). Accept both, keep the
	// primary subtag.
	tag = strings.ReplaceAll(tag, "_", "-")
	primary := tag
	if i := strings.IndexByte(tag, '-'); i >= 0 {
		primary = tag[:i]
	}
	primary = strings.ToLower(primary)
	if !isAlphaSubtag(primary) {
		return "", ErrUnsupportedLocale
	}
	return Locale(primary), nil
}

func isAlphaSubtag(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// ResolveLocale runs a two-link fallback chain — requested, then stored —
// falling back to def when neither parses. Each link is validated by
// ParseLocale; an unsupported tag falls through to the next rather than
// propagating.
func ResolveLocale(requested, stored string, def Locale) Locale {
	if loc, err := ParseLocale(requested); err == nil {
		return loc
	}
	if loc, err := ParseLocale(stored); err == nil {
		return loc
	}
	return def
}
