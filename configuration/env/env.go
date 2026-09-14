// Package env provides small, testable helpers for reading process
// environment configuration.
package env

import (
	"os"
	"strconv"
)

// Env abstracts environment lookups so consumers can mock configuration in
// tests instead of depending on process-global state.
type Env interface {
	// Get returns the raw value of the named environment variable, or "" if unset.
	Get(name string) string
	// GetOrDefault returns the named environment variable, or def if unset or empty.
	GetOrDefault(name, def string) string
	// GetBool parses the named environment variable as a bool, returning def
	// if unset or unparsable.
	GetBool(name string, def bool) bool
	// GetInt parses the named environment variable as an int, returning def
	// if unset or unparsable.
	GetInt(name string, def int) int
	// GetEnvironmentName returns APP_ENV, defaulting to "local" if unset.
	GetEnvironmentName() string
	// IsProduction reports whether GetEnvironmentName() == "production".
	IsProduction() bool
}

type envModule struct{}

// NewEnv returns the default Env implementation backed by os.Getenv.
func NewEnv() Env {
	return &envModule{}
}

func (*envModule) Get(name string) string {
	return os.Getenv(name)
}

func (e *envModule) GetOrDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func (e *envModule) GetBool(name string, def bool) bool {
	v, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func (e *envModule) GetInt(name string, def int) int {
	v, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func (e *envModule) GetEnvironmentName() string {
	return e.GetOrDefault("APP_ENV", "local")
}

func (e *envModule) IsProduction() bool {
	return e.GetEnvironmentName() == "production"
}
