// Package storage is the blob/object storage port: a file and document object
// service that mints opaque, server-controlled storage references and signed
// URLs, so a client never supplies a key that could point an upload or
// download at an arbitrary object.
//
// Typical flow:
//
//	intent, _ := s.CreateUploadIntent(ctx, "kyc_document", "image/png", 5<<20)
//	// client PUTs the bytes to intent.SignedPutURL ...
//	obj, _ := s.ConfirmUpload(ctx, intent.StorageRef)
//	// later, to serve the file:
//	url, _ := s.GetDownloadURL(ctx, obj.StorageRef, 15*time.Minute)
//
// This package holds only the interface, shared domain types, ref/policy
// helpers built on the standard library, and sentinel errors — no third-party
// import. See datastore/storage/gcs for the Google Cloud Storage adapter.
package storage

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by Storage implementations, matched with
// errors.Is; adapters wrap these with %w plus context.
var (
	// ErrUnknownKind rejects a kind the storage was not configured for.
	ErrUnknownKind = errors.New("storage: unknown kind")
	// ErrContentTypeNotAllowed rejects a declared or sniffed content type the
	// kind's policy does not permit.
	ErrContentTypeNotAllowed = errors.New("storage: content type not allowed for kind")
	// ErrTooLarge rejects a stored object over the effective cap for its kind.
	ErrTooLarge = errors.New("storage: object exceeds maximum size")
	// ErrNotFound reports a storageRef whose object is absent from the bucket.
	ErrNotFound = errors.New("storage: object not found")
	// ErrInvalidRef rejects a storageRef that does not decode to a known key.
	ErrInvalidRef = errors.New("storage: invalid storage reference")
	// ErrSignedURLUnsupported reports a backend without signed-URL support.
	ErrSignedURLUnsupported = errors.New("storage: signed URLs not supported by backend")
	// ErrInvalidConfig rejects constructor configuration such as no kinds or
	// an invalid KindPolicy.
	ErrInvalidConfig = errors.New("storage: invalid configuration")
)

// Storage is the port for a file/document object service. Implementations
// are safe for concurrent use by multiple goroutines.
type Storage interface {
	// CreateUploadIntent reserves an opaque storage reference for a single
	// upload and returns a short-lived signed PUT URL for writing the bytes
	// directly to object storage.
	//
	// kind selects a registered policy (allowed content types and maximum
	// size). contentType must be allowed by that policy. maxSize, when > 0,
	// may only lower the kind's cap for this intent, never raise it.
	CreateUploadIntent(ctx context.Context, kind, contentType string, maxSize int64) (UploadIntent, error)

	// ConfirmUpload is the gate before a storageRef counts as an attached
	// file: it verifies the object exists, sniffs its real MIME type from
	// the stored bytes rather than trusting the declared Content-Type, and
	// checks type and size against the ref's kind policy.
	ConfirmUpload(ctx context.Context, storageRef string) (FileObject, error)

	// GetDownloadURL returns a signed, time-limited GET URL for an existing,
	// confirmed object. ttl <= 0 applies a backend-configured default.
	GetDownloadURL(ctx context.Context, storageRef string, ttl time.Duration) (string, error)

	// Download reads an existing object server-side, for processors such as
	// OCR or virus scanning. An object over the ref's kind cap is rejected
	// with ErrTooLarge instead of read into memory.
	Download(ctx context.Context, storageRef string) ([]byte, error)

	// Close releases resources held by the underlying backend connection.
	Close() error
}

// UploadIntent is the result of CreateUploadIntent: an opaque server-minted
// reference plus the signed URL the client PUTs the file bytes to.
type UploadIntent struct {
	// StorageRef is the opaque, server-controlled identifier for the object
	// and the only handle a client holds.
	StorageRef string
	// SignedPutURL is the short-lived URL the client PUTs the bytes to.
	SignedPutURL string
	// ContentType is the Content-Type the client uses for the PUT.
	ContentType string
	// MaxSize is the largest size in bytes ConfirmUpload accepts for this object.
	MaxSize int64
	// ExpiresAt is when the signed PUT URL stops being valid.
	ExpiresAt time.Time
}

// FileObject is the confirmed metadata for a stored object.
type FileObject struct {
	// StorageRef is the opaque server-controlled identifier for the object.
	StorageRef string
	// Kind is the policy bucket the object belongs to (e.g. "kyc_document").
	Kind string
	// ContentType is sniffed from the stored bytes at confirm time; it is
	// authoritative and may differ from the client-declared type.
	ContentType string
	// Size is the stored object's size in bytes.
	Size int64
}

// KindPolicy is a named upload policy: the allowed content types and the
// maximum object size for one category of file (e.g. "kyc_document").
type KindPolicy struct {
	// Name identifies the policy and prefixes the object key; required, unique.
	Name string
	// AllowedContentTypes are matched exactly, case-insensitively; "*/*"
	// allows any type. Required, non-empty.
	AllowedContentTypes []string
	// MaxSize is the absolute cap in bytes; must be > 0.
	MaxSize int64
}

// Allows reports whether contentType is permitted by p, case-insensitively.
func (p KindPolicy) Allows(contentType string) bool {
	normalized := NormalizeContentType(contentType)
	for _, allowed := range p.AllowedContentTypes {
		a := NormalizeContentType(allowed)
		if a == "*/*" || a == normalized {
			return true
		}
	}
	return false
}

// Clock is the minimal seam a Storage implementation needs for deterministic
// tests; a nil Clock in Options means "use the real wall clock."
type Clock interface {
	Now() time.Time
}

// Options configures a Storage implementation, independent of which backend
// adapter constructs it.
type Options struct {
	// Kinds is the set of upload policies the storage enforces. Required,
	// at least one.
	Kinds []KindPolicy
	// Clock is injected for deterministic tests; nil uses the real clock.
	Clock Clock
	// UploadURLTTL bounds the lifetime of signed PUT URLs. Adapters default
	// this when <= 0.
	UploadURLTTL time.Duration
	// DownloadURLTTL is the default lifetime GetDownloadURL uses when the
	// caller passes ttl <= 0. Adapters default this when <= 0.
	DownloadURLTTL time.Duration
}
