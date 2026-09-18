package openfeature_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ofeature "github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"

	"github.com/adehikmatfr/go-pkg/v2/featureflag"
	"github.com/adehikmatfr/go-pkg/v2/featureflag/openfeature"
)

// slowProvider delays every boolean evaluation by delay, to exercise the
// adapter's evaluation timeout.
type slowProvider struct {
	ofeature.NoopProvider
	delay time.Duration
}

func (p slowProvider) BooleanEvaluation(ctx context.Context, _ string, def bool, _ ofeature.FlattenedContext) ofeature.BoolResolutionDetail {
	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
	}
	return ofeature.BoolResolutionDetail{
		Value:                    !def,
		ProviderResolutionDetail: ofeature.ProviderResolutionDetail{Reason: ofeature.StaticReason},
	}
}

// panicProvider panics on every boolean evaluation, to exercise the
// adapter's panic recovery.
type panicProvider struct {
	ofeature.NoopProvider
}

func (panicProvider) BooleanEvaluation(context.Context, string, bool, ofeature.FlattenedContext) ofeature.BoolResolutionDetail {
	panic("boom")
}

// targetingProvider resolves a boolean flag to true only when the flattened
// context carries a specific attribute/value pair, so toVendorContext's
// EvaluationContext mapping can be exercised end-to-end.
type targetingProvider struct {
	ofeature.NoopProvider
	wantKey   string
	wantValue any
}

func (p targetingProvider) BooleanEvaluation(_ context.Context, _ string, def bool, flatCtx ofeature.FlattenedContext) ofeature.BoolResolutionDetail {
	match := flatCtx[p.wantKey] == p.wantValue
	return ofeature.BoolResolutionDetail{
		Value:                    match,
		ProviderResolutionDetail: ofeature.ProviderResolutionDetail{Reason: ofeature.StaticReason},
	}
}

func newInMemoryProvider(t *testing.T) ofeature.FeatureProvider {
	t.Helper()
	return memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{
		"trading.enabled": {
			Key:            "trading.enabled",
			State:          memprovider.Enabled,
			DefaultVariant: "value",
			Variants:       map[string]any{"value": true},
		},
		"legacy.path": {
			Key:            "legacy.path",
			State:          memprovider.Disabled,
			DefaultVariant: "value",
			Variants:       map[string]any{"value": true},
		},
		"order.routing": {
			Key:            "order.routing",
			State:          memprovider.Enabled,
			DefaultVariant: "value",
			Variants:       map[string]any{"value": "venue-a"},
		},
	})
}

func TestNew_NilProvider(t *testing.T) {
	_, err := openfeature.New(openfeature.Config{})
	if !errors.Is(err, featureflag.ErrInvalidConfig) {
		t.Fatalf("got err %v, want it to wrap featureflag.ErrInvalidConfig", err)
	}
}

func TestBoolFlag(t *testing.T) {
	evaluator, err := openfeature.New(openfeature.Config{Provider: newInMemoryProvider(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = evaluator.Close(context.Background()) })

	tests := []struct {
		name string
		key  string
		def  bool
		want bool
	}{
		{name: "enabled flag resolves its value", key: "trading.enabled", def: false, want: true},
		{name: "disabled flag falls back to default", key: "legacy.path", def: false, want: false},
		{name: "unknown flag falls back to default", key: "does.not.exist", def: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluator.BoolFlag(context.Background(), tt.key, tt.def, featureflag.EvaluationContext{})
			if got != tt.want {
				t.Errorf("BoolFlag(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestStringFlag(t *testing.T) {
	evaluator, err := openfeature.New(openfeature.Config{Provider: newInMemoryProvider(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = evaluator.Close(context.Background()) })

	got := evaluator.StringFlag(context.Background(), "order.routing", "fallback", featureflag.EvaluationContext{})
	if got != "venue-a" {
		t.Errorf("StringFlag() = %q, want %q", got, "venue-a")
	}

	got = evaluator.StringFlag(context.Background(), "does.not.exist", "fallback", featureflag.EvaluationContext{})
	if got != "fallback" {
		t.Errorf("StringFlag() for an unknown flag = %q, want the default %q", got, "fallback")
	}
}

func TestBoolFlag_FailSafeOnTimeout(t *testing.T) {
	evaluator, err := openfeature.New(openfeature.Config{
		Provider:    slowProvider{delay: 100 * time.Millisecond},
		EvalTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := evaluator.BoolFlag(context.Background(), "any", true, featureflag.EvaluationContext{})
	if got != true {
		t.Errorf("BoolFlag() under a provider timeout = %v, want the default %v", got, true)
	}
}

func TestBoolFlag_FailSafeOnPanic(t *testing.T) {
	evaluator, err := openfeature.New(openfeature.Config{Provider: panicProvider{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := evaluator.BoolFlag(context.Background(), "any", true, featureflag.EvaluationContext{})
	if got != true {
		t.Errorf("BoolFlag() under a panicking provider = %v, want the default %v", got, true)
	}
}

func TestBoolFlag_EvaluationContextMapping(t *testing.T) {
	evaluator, err := openfeature.New(openfeature.Config{
		Provider: targetingProvider{wantKey: featureflag.AttrTier, wantValue: "gold"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := evaluator.BoolFlag(context.Background(), "any", false, featureflag.EvaluationContext{
		ActorID: "user-1",
		Tier:    "gold",
	})
	if got != true {
		t.Errorf("BoolFlag() with a matching Tier = %v, want true (the targeting provider should have matched)", got)
	}

	got = evaluator.BoolFlag(context.Background(), "any", false, featureflag.EvaluationContext{
		ActorID: "user-1",
		Tier:    "retail",
	})
	if got != false {
		t.Errorf("BoolFlag() with a non-matching Tier = %v, want false", got)
	}
}

func TestClose(t *testing.T) {
	evaluator, err := openfeature.New(openfeature.Config{Provider: newInMemoryProvider(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := evaluator.Close(context.Background()); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
	// Idempotent.
	if err := evaluator.Close(context.Background()); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}
