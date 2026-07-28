// Package logger configures a global zerolog logger that splits output by
// level: debug/info/warn go to stdout, error/fatal go to stderr. This keeps
// stdout usable for normal operational logs while letting log aggregators
// (or shell redirection) treat stderr as the error/alerting stream.
package logger

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Config controls the global logger initialized by InitGlobalLogger.
type Config struct {
	ServiceName string
	Level       zerolog.Level
}

// InitGlobalLogger replaces the zerolog global logger (log.Logger) with one
// configured per cfg. Call this once at process startup, before any other
// package logs.
func InitGlobalLogger(cfg *Config) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(cfg.Level)

	errWriter := &errorLevelWriter{zerolog.MultiLevelWriter(os.Stderr)}
	infoWriter := &belowErrorLevelWriter{zerolog.MultiLevelWriter(os.Stdout)}

	writer := zerolog.MultiLevelWriter(errWriter, infoWriter)
	log.Logger = zerolog.New(writer).With().Timestamp().Caller().Str("service", cfg.ServiceName).Logger()
}

// SetGlobalLevel changes the minimum level zerolog emits. Safe to call after
// InitGlobalLogger, e.g. to raise verbosity at runtime.
func SetGlobalLevel(lvl zerolog.Level) {
	zerolog.SetGlobalLevel(lvl)
}
