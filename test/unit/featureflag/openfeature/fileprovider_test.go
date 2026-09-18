package openfeature_test

import (
	"context"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/featureflag"
	"github.com/adehikmatfr/go-pkg/v2/featureflag/openfeature"
)

func TestNewFileProvider(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "valid file", path: "testdata/flags.json"},
		{name: "missing file", path: "testdata/does-not-exist.json", wantErr: true},
		{name: "malformed json", path: "testdata/malformed.json", wantErr: true},
		{name: "type mismatch", path: "testdata/type_mismatch.json", wantErr: true},
		{name: "unknown type", path: "testdata/unknown_type.json", wantErr: true},
		{name: "unknown field", path: "testdata/unknown_field.json", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := openfeature.NewFileProvider(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if provider == nil {
				t.Fatalf("expected a non-nil provider")
			}
		})
	}
}

func TestNewFileBackedFlags(t *testing.T) {
	t.Run("valid file resolves flags", func(t *testing.T) {
		evaluator, err := openfeature.NewFileBackedFlags(openfeature.Config{}, "testdata/flags.json")
		if err != nil {
			t.Fatalf("NewFileBackedFlags: %v", err)
		}
		t.Cleanup(func() { _ = evaluator.Close(context.Background()) })

		if got := evaluator.BoolFlag(context.Background(), "trading.enabled", false, featureflag.EvaluationContext{}); got != true {
			t.Errorf("BoolFlag(trading.enabled) = %v, want true", got)
		}
		if got := evaluator.BoolFlag(context.Background(), "legacy.path", false, featureflag.EvaluationContext{}); got != false {
			t.Errorf("BoolFlag(legacy.path) = %v, want the default false (flag is disabled)", got)
		}
		if got := evaluator.StringFlag(context.Background(), "order.routing", "fallback", featureflag.EvaluationContext{}); got != "venue-a" {
			t.Errorf("StringFlag(order.routing) = %q, want %q", got, "venue-a")
		}
	})

	t.Run("invalid file propagates the error", func(t *testing.T) {
		if _, err := openfeature.NewFileBackedFlags(openfeature.Config{}, "testdata/malformed.json"); err == nil {
			t.Fatalf("expected an error")
		}
	})
}
