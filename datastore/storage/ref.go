package storage

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path"
	"strings"
)

// refBytes is the number of random bytes used to make a storageRef
// unguessable: 16 bytes = 128 bits of entropy, base64url-encoded.
const refBytes = 16

// refSep separates the kind from the random token inside the encoded payload.
const refSep = ":"

// ValidateKinds checks kinds for the constraints every Storage adapter
// requires (non-empty, unique Name; non-empty AllowedContentTypes; MaxSize >
// 0) and returns them indexed by Name. Adapters call this from their
// constructor so the validation rule is identical across backends.
func ValidateKinds(kinds []KindPolicy) (map[string]KindPolicy, error) {
	if len(kinds) == 0 {
		return nil, fmt.Errorf("%w: at least one kind is required", ErrInvalidConfig)
	}
	policies := make(map[string]KindPolicy, len(kinds))
	for _, k := range kinds {
		if k.Name == "" {
			return nil, fmt.Errorf("%w: kind name must not be empty", ErrInvalidConfig)
		}
		if _, dup := policies[k.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate kind %q", ErrInvalidConfig, k.Name)
		}
		if len(k.AllowedContentTypes) == 0 {
			return nil, fmt.Errorf("%w: kind %q must allow at least one content type", ErrInvalidConfig, k.Name)
		}
		if k.MaxSize <= 0 {
			return nil, fmt.Errorf("%w: kind %q must have a positive MaxSize", ErrInvalidConfig, k.Name)
		}
		policies[k.Name] = k
	}
	return policies, nil
}

// NormalizeContentType lowercases s and strips any parameters after ';', so
// "Image/PNG; charset=binary" and "image/png" compare equal.
func NormalizeContentType(s string) string {
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// MintRef generates a fresh, unguessable object key and its opaque
// storageRef for kind. The key layout is "<kind>/<random>" and the ref
// encodes the same pair, so DecodeRef recovers the kind without a database
// round-trip while staying opaque to the client. Randomness comes from
// crypto/rand because the ref doubles as a capability: a predictable ref
// would let a caller guess other objects.
func MintRef(kind string) (key, ref string, err error) {
	buf := make([]byte, refBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("storage: generating storage reference: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	key = path.Join(kind, token)
	ref = encodeRef(kind, token)
	return key, ref, nil
}

// encodeRef produces the opaque, URL-safe storageRef for a kind+token pair.
func encodeRef(kind, token string) string {
	payload := kind + refSep + token
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeRef recovers the object key and kind from an opaque storageRef,
// returning ErrInvalidRef for anything malformed. A caller must still check
// the returned kind against its own configured policies — DecodeRef only
// defends against a ref that isn't shaped like one MintRef produced, so a
// corrupt or attacker-supplied ref can never be coerced into an arbitrary
// object key.
func DecodeRef(ref string) (key, kind string, err error) {
	if ref == "" {
		return "", "", fmt.Errorf("%w: empty", ErrInvalidRef)
	}
	raw, decErr := base64.RawURLEncoding.DecodeString(ref)
	if decErr != nil {
		return "", "", fmt.Errorf("%w: not base64", ErrInvalidRef)
	}
	kind, token, ok := strings.Cut(string(raw), refSep)
	if !ok || kind == "" || token == "" {
		return "", "", fmt.Errorf("%w: malformed payload", ErrInvalidRef)
	}
	// Both components come from encodeRef, so a slash or ".." in either is
	// smuggled traversal.
	if strings.ContainsAny(kind, "/.") || strings.ContainsAny(token, "/.") {
		return "", "", fmt.Errorf("%w: illegal characters", ErrInvalidRef)
	}
	return path.Join(kind, token), kind, nil
}
