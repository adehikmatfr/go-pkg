package logger_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/adehikmatfr/go-pkg/v2/observability/logger"
)

// captureStdoutStderr redirects the process's os.Stdout/os.Stderr to pipes
// for the duration of fn, returning what was written to each. This is the
// only way to observe InitGlobalLogger's stdout/stderr split from outside
// the package, since the writers that implement it are unexported.
func captureStdoutStderr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	origStdout, origStderr := os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	os.Stdout, os.Stderr = stdoutW, stderrW
	t.Cleanup(func() {
		os.Stdout, os.Stderr = origStdout, origStderr
	})

	fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()

	outBytes, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	errBytes, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	return string(outBytes), string(errBytes)
}

func TestInitGlobalLoggerSplitsOutputByLevel(t *testing.T) {
	stdout, stderr := captureStdoutStderr(t, func() {
		logger.InitGlobalLogger(&logger.Config{ServiceName: "test-service", Level: zerolog.DebugLevel})
		log.Info().Msg("info message")
		log.Warn().Msg("warn message")
		log.Error().Msg("error message")
	})

	if !strings.Contains(stdout, "info message") {
		t.Errorf("stdout = %q, want it to contain the info-level record", stdout)
	}
	if !strings.Contains(stdout, "warn message") {
		t.Errorf("stdout = %q, want it to contain the warn-level record", stdout)
	}
	if strings.Contains(stdout, "error message") {
		t.Errorf("stdout = %q, should not contain the error-level record", stdout)
	}

	if !strings.Contains(stderr, "error message") {
		t.Errorf("stderr = %q, want it to contain the error-level record", stderr)
	}
	if strings.Contains(stderr, "info message") || strings.Contains(stderr, "warn message") {
		t.Errorf("stderr = %q, should not contain below-error-level records", stderr)
	}
}

func TestInitGlobalLoggerSetsLevel(t *testing.T) {
	logger.InitGlobalLogger(&logger.Config{ServiceName: "test-service", Level: zerolog.WarnLevel})
	if zerolog.GlobalLevel() != zerolog.WarnLevel {
		t.Errorf("global level = %v, want %v", zerolog.GlobalLevel(), zerolog.WarnLevel)
	}

	logger.SetGlobalLevel(zerolog.DebugLevel)
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("global level after SetGlobalLevel = %v, want %v", zerolog.GlobalLevel(), zerolog.DebugLevel)
	}
}
