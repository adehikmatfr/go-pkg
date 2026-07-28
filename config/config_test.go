package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFileName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.local.yaml"), []byte("x: 1"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	// A same-prefixed file in a subdirectory must never be picked up.
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "app.local.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("failed to write nested fixture: %v", err)
	}

	name, err := resolveFileName(dir, "app", "local")
	if err != nil {
		t.Fatalf("resolveFileName() error: %v", err)
	}
	if name != "app.local.yaml" {
		t.Errorf("resolveFileName() = %q, want %q", name, "app.local.yaml")
	}
}

func TestResolveFileNameNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveFileName(dir, "app", "local"); !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("resolveFileName() error = %v, want %v", err, ErrConfigNotFound)
	}
}

type testConfig struct {
	Name string `fig:"name"`
	Port int    `fig:"port"`
}

func TestReadConfig(t *testing.T) {
	dir := t.TempDir()
	content := "name: svc\nport: 8080\n"
	if err := os.WriteFile(filepath.Join(dir, "app.local.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	t.Setenv("APP_ENV", "local")

	var cfg testConfig
	if err := NewConfig().ReadConfig(&cfg, dir, "app"); err != nil {
		t.Fatalf("ReadConfig() error: %v", err)
	}

	if cfg.Name != "svc" || cfg.Port != 8080 {
		t.Errorf("ReadConfig() = %+v, want Name=svc Port=8080", cfg)
	}
}

func TestReadConfigEnvOverride(t *testing.T) {
	dir := t.TempDir()
	content := "name: svc\nport: 8080\n"
	if err := os.WriteFile(filepath.Join(dir, "app.local.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	t.Setenv("APP_ENV", "local")
	t.Setenv("APP_PORT", "9090")

	var cfg testConfig
	if err := NewConfig().ReadConfig(&cfg, dir, "app"); err != nil {
		t.Fatalf("ReadConfig() error: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("ReadConfig() Port = %d, want env override 9090", cfg.Port)
	}
}

func TestReadConfigMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APP_ENV", "local")

	var cfg testConfig
	if err := NewConfig().ReadConfig(&cfg, dir, "app"); err == nil {
		t.Error("ReadConfig() should error when no config file exists")
	}
}

func TestReadConfigFromSpecifiedFile(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	configDir := filepath.Join(wd, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(configDir) })

	content := "name: svc\nport: 1234\n"
	if err := os.WriteFile(filepath.Join(configDir, "custom.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	var cfg testConfig
	if err := NewConfig().ReadConfigFromSpecifiedFile(&cfg, "custom.yaml"); err != nil {
		t.Fatalf("ReadConfigFromSpecifiedFile() error: %v", err)
	}
	if cfg.Name != "svc" || cfg.Port != 1234 {
		t.Errorf("ReadConfigFromSpecifiedFile() = %+v, want Name=svc Port=1234", cfg)
	}
}
