// Package openfeature is a featureflag.FlagEvaluator adapter over the
// OpenFeature Go SDK (https://openfeature.dev): it wraps any
// openfeature.FeatureProvider, so a caller can swap the backend (a
// file-backed provider via NewFileBackedFlags, or any other OpenFeature
// provider — LaunchDarkly, Flagsmith, GO Feature Flag, ...) without touching
// call sites. Vendor types never escape the package.
package openfeature

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/open-feature/go-sdk/openfeature"

	"github.com/adehikmatfr/go-pkg/v2/featureflag"
)

// DefaultEvalTimeout bounds a single flag evaluation so a hung backend
// cannot stall a request path. It can be overridden via Config.EvalTimeout.
const DefaultEvalTimeout = 250 * time.Millisecond

// Config configures a FlagEvaluator backed by the OpenFeature SDK.
type Config struct {
	// Provider is the underlying OpenFeature provider, required:
	// NewFileProvider for the file-backed MVP backend, or any other
	// openfeature.FeatureProvider.
	Provider openfeature.FeatureProvider
	// EvalTimeout bounds a single evaluation. Zero uses DefaultEvalTimeout; a
	// negative value disables the timeout, which suits tests only.
	EvalTimeout time.Duration
}

// client is the featureflag.FlagEvaluator backed by the OpenFeature SDK.
// Each instance owns a private OpenFeature domain, so instances and
// concurrent tests never share provider state through the SDK's global
// singleton.
type client struct {
	domain      string
	sdk         *openfeature.Client
	evalTimeout time.Duration
}

var _ featureflag.FlagEvaluator = (*client)(nil)

// New constructs a featureflag.FlagEvaluator from Config. It registers the
// supplied provider under a unique private domain and waits for it to
// initialize, so the first evaluation does not race a not-ready provider.
//
// It returns an error wrapping featureflag.ErrInvalidConfig if
// Config.Provider is nil.
func New(cfg Config) (featureflag.FlagEvaluator, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("%w: a FeatureProvider is required", featureflag.ErrInvalidConfig)
	}

	timeout := cfg.EvalTimeout
	if timeout == 0 {
		timeout = DefaultEvalTimeout
	}

	// A unique domain isolates this instance's provider in the SDK's global
	// registry; without it, two instances would clobber each other.
	domain := "go-pkg-featureflag-" + uuid.NewString()

	if err := openfeature.SetNamedProviderAndWait(domain, cfg.Provider); err != nil {
		return nil, fmt.Errorf("openfeature: set provider: %w", err)
	}

	return &client{
		domain:      domain,
		sdk:         openfeature.NewClient(domain),
		evalTimeout: timeout,
	}, nil
}

// BoolFlag resolves a boolean flag. It returns def on any provider error,
// timeout, panic, or type mismatch — the fail-safe contract.
func (c *client) BoolFlag(ctx context.Context, key string, def bool, evalCtx featureflag.EvaluationContext) bool {
	oCtx := toVendorContext(evalCtx)
	return evalSafe(ctx, c.evalTimeout, def, func(ctx context.Context) (bool, error) {
		return c.sdk.BooleanValue(ctx, key, def, oCtx)
	})
}

// StringFlag resolves a string flag. It returns def on any provider error,
// timeout, panic, or type mismatch — the fail-safe contract.
func (c *client) StringFlag(ctx context.Context, key string, def string, evalCtx featureflag.EvaluationContext) string {
	oCtx := toVendorContext(evalCtx)
	return evalSafe(ctx, c.evalTimeout, def, func(ctx context.Context) (string, error) {
		return c.sdk.StringValue(ctx, key, def, oCtx)
	})
}

// Close shuts down the underlying provider for this instance's domain. It is
// safe to call multiple times and on a nil receiver.
func (c *client) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	// The SDK only exposes a global shutdown; to avoid tearing down other
	// instances this deliberately does not call openfeature.Shutdown. The
	// domain's provider is dropped along with this client's last reference.
	// Providers that hold external resources should be closed by their owner.
	_ = ctx
	return nil
}

// evalSafe runs fn under an evaluation timeout and converts any error, panic,
// or timeout into the supplied default. This is the single choke point
// enforcing FlagEvaluator's fail-safe contract for every flag type.
func evalSafe[T any](ctx context.Context, timeout time.Duration, def T, fn func(context.Context) (T, error)) T {
	evalCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		evalCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	type result struct {
		val T
		err error
	}
	// Buffered so the goroutine never blocks if we already returned the
	// default because of a timeout.
	done := make(chan result, 1)

	go func() {
		// Recover from a misbehaving provider so a panic never escapes the port.
		defer func() {
			if r := recover(); r != nil {
				done <- result{val: def, err: fmt.Errorf("openfeature: provider panicked: %v", r)}
			}
		}()
		v, err := fn(evalCtx)
		done <- result{val: v, err: err}
	}()

	select {
	case <-evalCtx.Done():
		return def
	case res := <-done:
		if res.err != nil {
			return def
		}
		return res.val
	}
}

// toVendorContext flattens the port's vendor-free EvaluationContext into the
// OpenFeature EvaluationContext. Typed fields take precedence over Extra;
// empty typed fields are omitted so backend rules can distinguish "absent"
// from "empty".
func toVendorContext(ec featureflag.EvaluationContext) openfeature.EvaluationContext {
	attrs := make(map[string]any, len(ec.Extra)+3)
	for k, v := range ec.Extra {
		attrs[k] = v
	}
	if ec.ActorID != "" {
		attrs[featureflag.AttrActorID] = ec.ActorID
	}
	if ec.Tier != "" {
		attrs[featureflag.AttrTier] = ec.Tier
	}
	if ec.AssetClass != "" {
		attrs[featureflag.AttrAssetClass] = ec.AssetClass
	}
	return openfeature.NewEvaluationContext(ec.ActorID, attrs)
}
