package i18n_test

import (
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/i18n"
)

func TestParseLocale(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    i18n.Locale
		wantErr bool
	}{
		{name: "simple", tag: "en", want: "en"},
		{name: "region hyphen", tag: "en-US", want: "en"},
		{name: "region underscore", tag: "id_ID", want: "id"},
		{name: "mixed case", tag: "EN", want: "en"},
		{name: "whitespace", tag: "  en  ", want: "en"},
		{name: "empty", tag: "", wantErr: true},
		{name: "too long", tag: "english", wantErr: true},
		{name: "non alpha", tag: "12", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := i18n.ParseLocale(tt.tag)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseLocale(%q) error = %v, wantErr %v", tt.tag, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("ParseLocale(%q) = %q, want %q", tt.tag, got, tt.want)
			}
			if err != nil && !errors.Is(err, i18n.ErrUnsupportedLocale) {
				t.Errorf("ParseLocale(%q) error = %v, want wrapping ErrUnsupportedLocale", tt.tag, err)
			}
		})
	}
}

func TestResolveLocale(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		stored    string
		want      i18n.Locale
	}{
		{name: "requested wins", requested: "fr", stored: "en", want: "fr"},
		{name: "falls back to stored", requested: "", stored: "en", want: "en"},
		{name: "falls back to default", requested: "", stored: "", want: "en"},
		{name: "invalid requested falls through", requested: "???", stored: "id", want: "id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := i18n.ResolveLocale(tt.requested, tt.stored, "en")
			if got != tt.want {
				t.Errorf("ResolveLocale(%q, %q, en) = %q, want %q", tt.requested, tt.stored, got, tt.want)
			}
		})
	}
}

func TestBundleTranslate(t *testing.T) {
	b := i18n.NewBundle("en")
	if err := b.AddMessages("en", map[string]string{"greeting": "Hello, {{.Name}}!"}); err != nil {
		t.Fatalf("AddMessages() error: %v", err)
	}
	if err := b.AddMessages("id", map[string]string{"greeting": "Halo, {{.Name}}!"}); err != nil {
		t.Fatalf("AddMessages() error: %v", err)
	}

	got, err := b.Translate("greeting", "id", map[string]string{"Name": "Budi"})
	if err != nil {
		t.Fatalf("Translate() error: %v", err)
	}
	if got != "Halo, Budi!" {
		t.Errorf("Translate() = %q, want %q", got, "Halo, Budi!")
	}
}

func TestBundleFallsBackToBaseLocale(t *testing.T) {
	b := i18n.NewBundle("en")
	if err := b.AddMessages("en", map[string]string{"only_in_base": "base text"}); err != nil {
		t.Fatalf("AddMessages() error: %v", err)
	}

	got, err := b.Translate("only_in_base", "fr", nil)
	if err != nil {
		t.Fatalf("Translate() error: %v", err)
	}
	if got != "base text" {
		t.Errorf("Translate() = %q, want %q", got, "base text")
	}
}

func TestBundleMissingKeyEverywhere(t *testing.T) {
	b := i18n.NewBundle("en")
	if _, err := b.Translate("missing", "en", nil); !errors.Is(err, i18n.ErrMessageKeyNotFound) {
		t.Errorf("Translate() error = %v, want wrapping ErrMessageKeyNotFound", err)
	}
}

func TestBundleAddMessagesRejectsInvalidTemplate(t *testing.T) {
	b := i18n.NewBundle("en")
	err := b.AddMessages("en", map[string]string{"bad": "{{.Unterminated"})
	if err == nil {
		t.Fatal("AddMessages() with an invalid template should return an error")
	}
}

func TestBundleRenderFailsOnMissingData(t *testing.T) {
	b := i18n.NewBundle("en")
	if err := b.AddMessages("en", map[string]string{"greeting": "Hello, {{.Name}}!"}); err != nil {
		t.Fatalf("AddMessages() error: %v", err)
	}

	// Missing field access in text/template with a struct (not a map)
	// returns an execution error.
	type noName struct{ Other string }
	if _, err := b.Translate("greeting", "en", noName{Other: "x"}); !errors.Is(err, i18n.ErrMessageRenderFailed) {
		t.Errorf("Translate() error = %v, want wrapping ErrMessageRenderFailed", err)
	}
}
