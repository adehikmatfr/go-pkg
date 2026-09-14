package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/configuration/config"
)

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
	if err := config.NewConfig().ReadConfig(&cfg, dir, "app"); err != nil {
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
	if err := config.NewConfig().ReadConfig(&cfg, dir, "app"); err != nil {
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
	err := config.NewConfig().ReadConfig(&cfg, dir, "app")
	if !errors.Is(err, config.ErrConfigNotFound) {
		t.Errorf("ReadConfig() error = %v, want %v", err, config.ErrConfigNotFound)
	}
}

func TestReadConfigPrefersYamlOverOtherExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.local.yaml"), []byte("name: from-yaml\nport: 1\n"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.local.json"), []byte(`{"name":"from-json","port":2}`), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	t.Setenv("APP_ENV", "local")

	var cfg testConfig
	if err := config.NewConfig().ReadConfig(&cfg, dir, "app"); err != nil {
		t.Fatalf("ReadConfig() error: %v", err)
	}
	if cfg.Name != "from-yaml" {
		t.Errorf("ReadConfig() picked Name=%q, want the .yaml file (from-yaml) over .json", cfg.Name)
	}
}

func TestReadConfigIgnoresNestedSubdirectory(t *testing.T) {
	// A same-prefixed file in a subdirectory must never be picked up.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.local.yaml"), []byte("name: top-level\nport: 1\n"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "app.local.json"), []byte(`{"name":"nested-decoy","port":99}`), 0o600); err != nil {
		t.Fatalf("failed to write nested fixture: %v", err)
	}

	t.Setenv("APP_ENV", "local")

	var cfg testConfig
	if err := config.NewConfig().ReadConfig(&cfg, dir, "app"); err != nil {
		t.Fatalf("ReadConfig() error: %v", err)
	}
	if cfg.Name != "top-level" {
		t.Errorf("ReadConfig() picked Name=%q, want the top-level file, not the nested decoy", cfg.Name)
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
	if err := config.NewConfig().ReadConfigFromSpecifiedFile(&cfg, "custom.yaml"); err != nil {
		t.Fatalf("ReadConfigFromSpecifiedFile() error: %v", err)
	}
	if cfg.Name != "svc" || cfg.Port != 1234 {
		t.Errorf("ReadConfigFromSpecifiedFile() = %+v, want Name=svc Port=1234", cfg)
	}
}
