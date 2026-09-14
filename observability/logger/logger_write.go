package logger

import "github.com/rs/zerolog"

// belowErrorLevelWriter forwards only debug/info/warn records (level < Error).
type belowErrorLevelWriter struct {
	w zerolog.LevelWriter
}

// Write exists only to satisfy zerolog.LevelWriter's embedded io.Writer;
// zerolog always dispatches to WriteLevel when a writer implements
// LevelWriter (as this one does), so this is never called through
// InitGlobalLogger, this package's only entry point.
func (lw *belowErrorLevelWriter) Write(p []byte) (int, error) { // coverage-ignore
	return lw.w.Write(p)
}

func (lw *belowErrorLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if level < zerolog.ErrorLevel {
		return lw.w.WriteLevel(level, p)
	}
	return len(p), nil
}

// errorLevelWriter forwards only error/fatal/panic records (level >= Error).
type errorLevelWriter struct {
	w zerolog.LevelWriter
}

// Write exists only to satisfy zerolog.LevelWriter's embedded io.Writer; see
// belowErrorLevelWriter.Write for why it's never actually called.
func (lw *errorLevelWriter) Write(p []byte) (int, error) { // coverage-ignore
	return lw.w.Write(p)
}

func (lw *errorLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if level >= zerolog.ErrorLevel {
		return lw.w.WriteLevel(level, p)
	}
	return len(p), nil
}
