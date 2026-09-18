package openfeature

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"

	"github.com/adehikmatfr/go-pkg/v2/featureflag"
)

// flagType enumerates the value types supported by the file-backed provider.
type flagType string

const (
	flagTypeBoolean flagType = "boolean"
	flagTypeString  flagType = "string"
)

// fileFlag is the on-disk representation of a single flag.
type fileFlag struct {
	// Type is "boolean" or "string".
	Type flagType `json:"type"`
	// Value is the flag's value; its concrete JSON type must match Type.
	Value any `json:"value"`
	// Disabled, when true, marks the flag as administratively off. A
	// disabled flag resolves to the caller's default (fail-safe), modelling
	// a committed kill-switch in the OFF position.
	Disabled bool `json:"disabled"`
}

// fileDocument is the on-disk schema for the committed flag file:
//
//	{
//	  "flags": {
//	    "trading.enabled":    { "type": "boolean", "value": true },
//	    "trading.killswitch": { "type": "boolean", "value": false },
//	    "order.routing":      { "type": "string",  "value": "venue-a" },
//	    "legacy.path":        { "type": "boolean", "value": true, "disabled": true }
//	  }
//	}
type fileDocument struct {
	Flags map[string]fileFlag `json:"flags"`
}

// NewFileProvider builds an in-process OpenFeature provider from a committed
// JSON flag file at path, so evaluation is deterministic and
// dependency-free. The result is passed as Config.Provider to New; the file
// is read once at construction and the provider is immutable thereafter.
//
// It returns an error if the file cannot be read, the JSON is malformed, a
// flag declares an unknown type, or a value does not match its declared
// type.
func NewFileProvider(path string) (openfeature.FeatureProvider, error) {
	// path is a deploy-time, version-controlled config path supplied by
	// operators (not end-user input); clean it and read once at construction.
	raw, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- trusted operator-provided config path
	if err != nil {
		return nil, fmt.Errorf("openfeature: read flag file %q: %w", path, err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var doc fileDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("openfeature: parse flag file %q: %w", path, err)
	}

	memFlags := make(map[string]memprovider.InMemoryFlag, len(doc.Flags))
	for key, ff := range doc.Flags {
		mf, err := toMemoryFlag(key, ff)
		if err != nil {
			return nil, err
		}
		memFlags[key] = mf
	}

	return memprovider.NewInMemoryProvider(memFlags), nil
}

// toMemoryFlag validates one file flag and converts it to the vendor flag
// type. Type/value mismatches are rejected here so a bad flag file fails
// fast at startup rather than silently resolving to defaults at runtime.
func toMemoryFlag(key string, ff fileFlag) (memprovider.InMemoryFlag, error) {
	state := memprovider.Enabled
	if ff.Disabled {
		state = memprovider.Disabled
	}

	switch ff.Type {
	case flagTypeBoolean:
		b, ok := ff.Value.(bool)
		if !ok {
			return memprovider.InMemoryFlag{}, fmt.Errorf(
				"openfeature: flag %q declared boolean but value is %T", key, ff.Value)
		}
		return memprovider.InMemoryFlag{
			Key:            key,
			State:          state,
			DefaultVariant: "value",
			Variants:       map[string]any{"value": b},
		}, nil

	case flagTypeString:
		s, ok := ff.Value.(string)
		if !ok {
			return memprovider.InMemoryFlag{}, fmt.Errorf(
				"openfeature: flag %q declared string but value is %T", key, ff.Value)
		}
		return memprovider.InMemoryFlag{
			Key:            key,
			State:          state,
			DefaultVariant: "value",
			Variants:       map[string]any{"value": s},
		}, nil

	default:
		return memprovider.InMemoryFlag{}, fmt.Errorf(
			"openfeature: flag %q has unknown type %q (want boolean or string)", key, ff.Type)
	}
}

// NewFileBackedFlags is a convenience constructor that builds a file-backed
// provider from path and wires it into a FlagEvaluator using cfg. Any
// Provider set on cfg is ignored in favour of the file provider.
func NewFileBackedFlags(cfg Config, path string) (featureflag.FlagEvaluator, error) {
	provider, err := NewFileProvider(path)
	if err != nil {
		return nil, err
	}
	cfg.Provider = provider
	return New(cfg)
}
