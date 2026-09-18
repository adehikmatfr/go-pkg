// Package audit is the tamper-evident audit-trail port: an immutable,
// append-only AuditStore consumers depend on, plus the AuditEvent domain
// type and the hash-chain helpers every implementation shares. No
// third-party import — a concrete adapter (e.g. audit/memory) keeps any
// vendor types internal.
//
// Each event's row hash covers the event's canonical JSON plus the previous
// row's hash, so a later edit, reorder, or deletion is detectable by
// recomputing the chain (see VerifyChain). The canonical form sorts object
// keys recursively, so a verifier reproduces every hash byte-for-byte
// regardless of map iteration order.
//
// Records are tamper-evident, not confidential: Data should carry
// references and opaque hashes only — no PII, secrets, tokens, or raw
// monetary instructions.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// GenesisHash is the previous-hash value for the first link in a chain, so
// the first row's hash is sha256(canonicalJSON(event)).
const GenesisHash = ""

// Sentinel errors, matched with errors.Is.
var (
	// ErrMissingID rejects an event without a stable id, which would make
	// the chain non-reproducible.
	ErrMissingID = errors.New("audit: event id is required")
	// ErrMissingSource rejects an event without a source attribute.
	ErrMissingSource = errors.New("audit: event source is required")
	// ErrMissingType rejects an event without a type attribute.
	ErrMissingType = errors.New("audit: event type is required")
	// ErrChainBroken reports a link whose stored hash differs from the
	// recomputed one — evidence of tampering.
	ErrChainBroken = errors.New("audit: hash chain broken")
)

// AuditEvent is the immutable, non-PII record of a single privileged or
// financial action.
type AuditEvent struct {
	// ID is a globally unique, stable identifier. With Source it identifies
	// the occurrence, and it is the idempotency key for appends.
	ID string
	// Source is the context the event happened in, e.g. "/identity/login".
	Source string
	// Type is the reverse-DNS action kind, e.g. "tech.example.order.placed".
	Type string
	// Subject is the targeted resource as an opaque reference, never PII.
	Subject string
	// Time is the occurrence time, stored in UTC.
	Time time.Time
	// Data carries references and hashes describing what changed — no PII,
	// secrets, tokens, or raw monetary instructions. Keys are sorted during
	// canonicalization.
	Data map[string]any
}

// Validate reports whether the event carries the attributes a reproducible
// chain needs, returning the sentinel for the first missing one.
func (e AuditEvent) Validate() error {
	if e.ID == "" {
		return ErrMissingID
	}
	if e.Source == "" {
		return ErrMissingSource
	}
	if e.Type == "" {
		return ErrMissingType
	}
	return nil
}

// CanonicalJSON returns the hash pre-image: object keys sorted
// lexicographically at every level, time normalized to UTC RFC3339Nano, no
// insignificant whitespace — so any verifier reproduces it byte-for-byte.
func (e AuditEvent) CanonicalJSON() ([]byte, error) {
	// A plain map so key ordering runs through the recursive canonicalizer.
	m := map[string]any{
		"id":     e.ID,
		"source": e.Source,
		"type":   e.Type,
		"time":   e.Time.UTC().Format(time.RFC3339Nano),
	}
	if e.Subject != "" {
		m["subject"] = e.Subject
	}
	if len(e.Data) > 0 {
		// Round-trip so numerics and nested values canonicalize as a
		// verifier reading from storage sees them.
		normalized, err := normalizeJSONValue(e.Data)
		if err != nil {
			return nil, fmt.Errorf("audit: canonicalize data: %w", err)
		}
		m["data"] = normalized
	}

	canon, err := canonicalize(m)
	if err != nil {
		return nil, fmt.Errorf("audit: canonicalize event: %w", err)
	}
	return canon, nil
}

// normalizeJSONValue round-trips v through encoding/json so concrete Go
// types collapse into the canonical JSON value space, keeping the form
// stable across producers.
func normalizeJSONValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// canonicalize marshals v to JSON with all object keys sorted recursively.
func canonicalize(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		buf := make([]byte, 0, 64)
		buf = append(buf, '{')
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kb...)
			buf = append(buf, ':')
			vb, err := canonicalize(t[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, vb...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := make([]byte, 0, 32)
		buf = append(buf, '[')
		for i, item := range t {
			if i > 0 {
				buf = append(buf, ',')
			}
			ib, err := canonicalize(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, ib...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		// Scalars already have one canonical JSON form.
		return json.Marshal(v)
	}
}

// ComputeRowHash returns the chain hash for an event given the previous
// row's hash: rowHash = hex(sha256(canonicalJSON(event) || prevHash)). The
// first row in a chain uses GenesisHash as prevHash.
func ComputeRowHash(prevHash string, e AuditEvent) (string, error) {
	canon, err := e.CanonicalJSON()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(canon)
	h.Write([]byte(prevHash))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Record is a stored chain link: the event together with the previous and
// current row hashes. VerifyChain operates on a slice of these.
type Record struct {
	// PrevHash is the hash of the preceding link (GenesisHash for the first).
	PrevHash string
	// RowHash is sha256(canonicalJSON(Event) || PrevHash), hex-encoded.
	RowHash string
	// Event is the immutable audit event for this link.
	Event AuditEvent
}

// AuditStore is the port for appending to and verifying a tamper-evident
// audit chain. Implementations are append-only; existing records are never
// mutated.
type AuditStore interface {
	// Append persists event as the next link after prevHash, the current
	// tail's RowHash, and returns its rowHash. A mismatch is rejected, so
	// concurrent or out-of-order writers cannot fork the chain. Idempotent
	// on the event ID: re-appending a recorded event returns the existing
	// rowHash.
	Append(ctx context.Context, prevHash string, event AuditEvent) (rowHash string, err error)

	// VerifyChain recomputes every link over the ordered records and
	// confirms the stored RowHash and PrevHash linkage, returning an error
	// wrapping ErrChainBroken at the first bad link.
	VerifyChain(ctx context.Context, records []Record) error
}

// VerifyChain checks a chain independently of any store: record[0].PrevHash
// is GenesisHash, each PrevHash equals the previous RowHash, and each
// RowHash equals ComputeRowHash(PrevHash, Event). The first failure wraps
// ErrChainBroken; an empty slice is a valid chain.
func VerifyChain(records []Record) error {
	prev := GenesisHash
	for i, rec := range records {
		if rec.PrevHash != prev {
			return fmt.Errorf("%w: link %d prev hash mismatch (expected %q, got %q)",
				ErrChainBroken, i, prev, rec.PrevHash)
		}
		want, err := ComputeRowHash(rec.PrevHash, rec.Event)
		if err != nil {
			return fmt.Errorf("audit: link %d: %w", i, err)
		}
		if rec.RowHash != want {
			return fmt.Errorf("%w: link %d row hash mismatch (event %q tampered)",
				ErrChainBroken, i, rec.Event.ID)
		}
		prev = rec.RowHash
	}
	return nil
}
