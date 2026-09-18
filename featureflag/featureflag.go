// Package featureflag is the feature-flag / kill-switch port: a vendor-free
// FlagEvaluator consumers depend on, plus the domain types it operates on. No
// third-party import — a concrete adapter (e.g. featureflag/openfeature)
// keeps its vendor types internal.
//
// Evaluation fails safe: every FlagEvaluator method returns the caller's
// default on any backend error, timeout, or panic, and never returns an
// error itself. Since flags commonly act as kill-switches, pass the
// conservative value as that default — the one that keeps the system safe
// while the flag backend is unavailable.
package featureflag

import (
	"context"
	"errors"
)

// ErrInvalidConfig indicates an adapter was constructed with incomplete or
// invalid configuration.
var ErrInvalidConfig = errors.New("featureflag: invalid config")

// Attribute keys the typed EvaluationContext fields are flattened under
// before reaching an adapter's backend, so backend targeting rules can name
// them.
const (
	// AttrActorID is the attribute key for EvaluationContext.ActorID.
	AttrActorID = "actorID"
	// AttrTier is the attribute key for EvaluationContext.Tier.
	AttrTier = "tier"
	// AttrAssetClass is the attribute key for EvaluationContext.AssetClass.
	AttrAssetClass = "assetClass"
)

// EvaluationContext carries the typed targeting information used to resolve
// a flag; it is vendor-free, so callers never build a backend-specific
// evaluation context. ActorID doubles as the targeting key, keeping
// percentage rollouts and per-actor overrides stable for a given actor.
type EvaluationContext struct {
	// ActorID identifies the subject of the evaluation: an end user or a
	// service account.
	ActorID string
	// Tier is the actor's account or service tier, e.g. "retail", "pro", "gold".
	Tier string
	// AssetClass scopes the evaluation to an asset class, e.g. "stock",
	// "crypto", so flags stay multi-asset aware without per-asset code paths.
	AssetClass string
	// Extra holds additional targeting attributes; the typed fields above win
	// on a key collision.
	Extra map[string]any
}

// FlagEvaluator is the feature-flag port; consumers depend on it, not on a
// concrete type or vendor SDK. Every evaluation method fails safe — it
// returns the supplied default on any error, timeout, panic, or type
// mismatch, and never returns an error or panics.
type FlagEvaluator interface {
	// BoolFlag resolves a boolean flag, returning def on any failure.
	BoolFlag(ctx context.Context, key string, def bool, evalCtx EvaluationContext) bool
	// StringFlag resolves a string flag, returning def on any failure.
	StringFlag(ctx context.Context, key string, def string, evalCtx EvaluationContext) string
	// Close releases any resources held for this evaluator. It is safe to
	// call multiple times.
	Close(ctx context.Context) error
}
