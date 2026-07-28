package logger

import "github.com/rs/zerolog"

// belowErrorLevelWriter forwards only debug/info/warn records (level < Error).
type belowErrorLevelWriter struct {
	w zerolog.LevelWriter
}

func (lw *belowErrorLevelWriter) Write(p []byte) (int, error) {
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

func (lw *errorLevelWriter) Write(p []byte) (int, error) {
	return lw.w.Write(p)
}

func (lw *errorLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if level >= zerolog.ErrorLevel {
		return lw.w.WriteLevel(level, p)
	}
	return len(p), nil
}
