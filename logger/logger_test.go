package logger

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
)

func TestBelowErrorLevelWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	w := &belowErrorLevelWriter{w: zerolog.MultiLevelWriter(buf)}

	cases := []struct {
		level      zerolog.Level
		wantWrites bool
	}{
		{zerolog.DebugLevel, true},
		{zerolog.InfoLevel, true},
		{zerolog.WarnLevel, true},
		{zerolog.ErrorLevel, false},
		{zerolog.FatalLevel, false},
	}

	for _, tc := range cases {
		buf.Reset()
		n, err := w.WriteLevel(tc.level, []byte("msg"))
		if err != nil {
			t.Fatalf("WriteLevel(%v) error: %v", tc.level, err)
		}
		if n != len("msg") {
			t.Errorf("WriteLevel(%v) n = %d, want %d", tc.level, n, len("msg"))
		}
		if got := buf.Len() > 0; got != tc.wantWrites {
			t.Errorf("WriteLevel(%v) wrote to underlying writer = %v, want %v", tc.level, got, tc.wantWrites)
		}
	}
}

func TestErrorLevelWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	w := &errorLevelWriter{w: zerolog.MultiLevelWriter(buf)}

	cases := []struct {
		level      zerolog.Level
		wantWrites bool
	}{
		{zerolog.DebugLevel, false},
		{zerolog.InfoLevel, false},
		{zerolog.WarnLevel, false},
		{zerolog.ErrorLevel, true},
		{zerolog.FatalLevel, true},
	}

	for _, tc := range cases {
		buf.Reset()
		n, err := w.WriteLevel(tc.level, []byte("msg"))
		if err != nil {
			t.Fatalf("WriteLevel(%v) error: %v", tc.level, err)
		}
		if n != len("msg") {
			t.Errorf("WriteLevel(%v) n = %d, want %d", tc.level, n, len("msg"))
		}
		if got := buf.Len() > 0; got != tc.wantWrites {
			t.Errorf("WriteLevel(%v) wrote to underlying writer = %v, want %v", tc.level, got, tc.wantWrites)
		}
	}
}

func TestInitGlobalLoggerSetsLevel(t *testing.T) {
	InitGlobalLogger(&Config{ServiceName: "test-service", Level: zerolog.WarnLevel})
	if zerolog.GlobalLevel() != zerolog.WarnLevel {
		t.Errorf("global level = %v, want %v", zerolog.GlobalLevel(), zerolog.WarnLevel)
	}

	SetGlobalLevel(zerolog.DebugLevel)
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("global level after SetGlobalLevel = %v, want %v", zerolog.GlobalLevel(), zerolog.DebugLevel)
	}
}
