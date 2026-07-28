// Package config loads YAML/JSON application configuration files (via
// github.com/kkyr/fig), resolving the file name from the current
// environment (APP_ENV) and allowing overrides via environment variables.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/adehikmatfr/go-pkg/env"
	"github.com/kkyr/fig"
)

// allowedExtensions lists the config file extensions this package will look
// for, in priority order.
var allowedExtensions = []string{".yaml", ".yml", ".json"}

// ErrConfigNotFound is returned when no file matching "<module>.<env><ext>"
// exists in dir for any allowed extension.
var ErrConfigNotFound = errors.New("config: no config file found for module in the given directory")

// Config loads structured configuration into a caller-provided struct.
type Config interface {
	// ReadConfig loads "<module>.<environment><ext>" from dir (environment
	// comes from APP_ENV, defaulting to "local") into cfg, applying
	// environment-variable overrides prefixed with the upper-cased module name.
	ReadConfig(cfg interface{}, dir string, module string) error
	// ReadConfigFromSpecifiedFile loads configFileName (resolved against the
	// working directory and the "config" directory) into cfg, with no
	// environment-variable override prefix.
	ReadConfigFromSpecifiedFile(cfg interface{}, configFileName string) error
}

type configModule struct{}

// NewConfig returns the default fig-backed Config implementation.
func NewConfig() Config {
	return &configModule{}
}

func (*configModule) ReadConfig(cfg interface{}, dir string, module string) error {
	environ := env.NewEnv().GetEnvironmentName()

	fileName, err := resolveFileName(dir, module, environ)
	if err != nil {
		return err
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	return fig.Load(cfg,
		fig.File(fileName),
		fig.UseEnv(strings.ToUpper(module)),
		fig.Dirs(wd, dir),
	)
}

func (*configModule) ReadConfigFromSpecifiedFile(cfg interface{}, configFileName string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	return fig.Load(cfg,
		fig.File(configFileName),
		fig.UseEnv(""),
		fig.Dirs(wd, "config"),
	)
}

// resolveFileName finds which of "<module>.<environ><ext>" (for each allowed
// extension) actually exists directly under dir. Unlike walking the whole
// directory tree, this only ever stats the small set of candidate file names
// this package would actually load, so it can't pick up an unrelated file
// from a nested subdirectory and its cost doesn't grow with directory size.
func resolveFileName(dir, module, environ string) (string, error) {
	for _, ext := range allowedExtensions {
		name := module + "." + environ + ext
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return name, nil
		}
	}
	return "", ErrConfigNotFound
}
