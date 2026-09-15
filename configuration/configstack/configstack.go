// Package configstack loads structured configuration from any number of
// layered YAML files plus a prefixed environment-variable overlay, using
// github.com/knadh/koanf.
//
// Unlike configuration/config's single "<module>.<APP_ENV>.yaml" file
// resolved by environment name, this package loads an explicit, ordered list
// of file paths (later files override earlier ones — e.g. a base file plus a
// per-environment override) and applies environment variables under a
// configured prefix as the final, highest-precedence layer.
//
// This package does not implement struct-tag defaults or required-field
// validation. Set defaults on the struct (or in a base YAML file loaded
// first) before calling Load, and validate the result yourself afterward if
// needed.
package configstack

import (
	"errors"
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	env "github.com/knadh/koanf/providers/env/v2"
	kfile "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// keyDelim addresses nested config keys ("db.dsn").
const keyDelim = "."

// structTagName is the struct field tag koanf reads to map keys to fields.
const structTagName = "koanf"

// ErrFileNotFound reports a YAML path passed to Load that cannot be read.
var ErrFileNotFound = errors.New("configstack: file not found")

// Loader is the port consumers depend on.
type Loader interface {
	// Load reads the YAML paths in order (later files override earlier
	// ones), overlays the environment, and unmarshals into out, a non-nil
	// pointer to a struct. Zero paths still applies the environment overlay.
	Load(out interface{}, paths ...string) error
}

// koanfLoader is the default Loader implementation.
type koanfLoader struct {
	envPrefix    string
	envNestDelim string
}

// Option customizes a Loader built by New.
type Option func(*koanfLoader)

// WithEnvPrefix sets the prefix used to select and strip environment
// variables, so WithEnvPrefix("APP_") maps APP_DB__DSN to db.dsn. Without a
// prefix the environment overlay is off, rather than consuming unrelated
// variables.
func WithEnvPrefix(prefix string) Option {
	return func(l *koanfLoader) { l.envPrefix = prefix }
}

// WithEnvNestDelim sets the delimiter that separates nested path segments
// within an environment variable name (after the prefix). The default is
// "__", so APP_DB__DSN maps to db.dsn.
func WithEnvNestDelim(delim string) Option {
	return func(l *koanfLoader) {
		if delim != "" {
			l.envNestDelim = delim
		}
	}
}

// New constructs a Loader. With no options it only loads YAML files; the
// environment overlay turns on once WithEnvPrefix configures a prefix.
func New(opts ...Option) Loader {
	l := &koanfLoader{envNestDelim: "__"}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Load implements Loader.
func (l *koanfLoader) Load(out interface{}, paths ...string) error {
	k := koanf.New(keyDelim)

	for _, p := range paths {
		if err := k.Load(kfile.Provider(p), yaml.Parser()); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrFileNotFound, p, err)
		}
	}

	if l.envPrefix != "" {
		provider := env.Provider(keyDelim, env.Opt{
			Prefix: l.envPrefix,
			TransformFunc: func(key, value string) (string, any) {
				key = strings.TrimPrefix(key, l.envPrefix)
				key = strings.ReplaceAll(key, l.envNestDelim, keyDelim)
				return strings.ToLower(key), value
			},
		})
		if err := k.Load(provider, nil); err != nil {
			return fmt.Errorf("configstack: load env: %w", err)
		}
	}

	if err := k.UnmarshalWithConf("", out, koanf.UnmarshalConf{Tag: structTagName}); err != nil {
		return fmt.Errorf("configstack: unmarshal: %w", err)
	}

	return nil
}
