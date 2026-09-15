package configstack_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/configuration/configstack"
)

type appConfig struct {
	Service  string `koanf:"service"`
	HTTPPort int    `koanf:"httpport"`
	DB       struct {
		DSN string `koanf:"dsn"`
	} `koanf:"db"`
}

func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadSingleFile(t *testing.T) {
	dir := t.TempDir()
	base := writeYAML(t, dir, "base.yaml", "service: identity\nhttpport: 8080\n")

	var cfg appConfig
	if err := configstack.New().Load(&cfg, base); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Service != "identity" || cfg.HTTPPort != 8080 {
		t.Errorf("cfg = %+v, want {Service:identity HTTPPort:8080}", cfg)
	}
}

func TestLoadLaterFileOverridesEarlier(t *testing.T) {
	dir := t.TempDir()
	base := writeYAML(t, dir, "base.yaml", "service: identity\nhttpport: 8080\n")
	override := writeYAML(t, dir, "override.yaml", "httpport: 9090\n")

	var cfg appConfig
	if err := configstack.New().Load(&cfg, base, override); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Service != "identity" {
		t.Errorf("cfg.Service = %q, want %q (from base, not overridden)", cfg.Service, "identity")
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("cfg.HTTPPort = %d, want %d (overridden)", cfg.HTTPPort, 9090)
	}
}

func TestLoadEnvOverlayTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	base := writeYAML(t, dir, "base.yaml", "service: identity\nhttpport: 8080\ndb:\n  dsn: file-dsn\n")

	t.Setenv("APP_HTTPPORT", "9999")
	t.Setenv("APP_DB__DSN", "env-dsn")

	var cfg appConfig
	err := configstack.New(configstack.WithEnvPrefix("APP_")).Load(&cfg, base)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.HTTPPort != 9999 {
		t.Errorf("cfg.HTTPPort = %d, want %d (from env)", cfg.HTTPPort, 9999)
	}
	if cfg.DB.DSN != "env-dsn" {
		t.Errorf("cfg.DB.DSN = %q, want %q (from env, nested via __)", cfg.DB.DSN, "env-dsn")
	}
	if cfg.Service != "identity" {
		t.Errorf("cfg.Service = %q, want %q (untouched by env)", cfg.Service, "identity")
	}
}

func TestLoadMissingFile(t *testing.T) {
	var cfg appConfig
	err := configstack.New().Load(&cfg, filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, configstack.ErrFileNotFound) {
		t.Errorf("Load() error = %v, want wrapping ErrFileNotFound", err)
	}
}

func TestLoadNoPathsAppliesEnvOnly(t *testing.T) {
	t.Setenv("APP_SERVICE", "identity")

	var cfg appConfig
	if err := configstack.New(configstack.WithEnvPrefix("APP_")).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Service != "identity" {
		t.Errorf("cfg.Service = %q, want %q", cfg.Service, "identity")
	}
}
