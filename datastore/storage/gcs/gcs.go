// Package gcs is a storage.Storage adapter over Google Cloud Storage, built
// on gocloud.dev/blob's gcsblob driver. NewClient dials a real GCS bucket
// (Application Default Credentials); NewFromBucket wraps an already-opened
// *blob.Bucket (any gocloud.dev driver — gcsblob in production, memblob or
// fileblob in tests), which is where every actual method implementation
// lives.
package gcs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"gocloud.dev/blob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/gcerrors"
	"gocloud.dev/gcp"

	"github.com/adehikmatfr/go-pkg/v2/datastore/storage"
)

// defaultDownloadTTL is applied when GetDownloadURL receives a non-positive ttl.
const defaultDownloadTTL = 15 * time.Minute

// defaultUploadTTL bounds how long a freshly minted signed PUT URL is valid.
const defaultUploadTTL = 15 * time.Minute

// sniffBytes is the number of leading bytes read to detect a MIME type. The
// mimetype library never needs more than 3072 bytes for its built-in matchers.
const sniffBytes = 3072

// Config configures the real GCS dial in NewClient.
type Config struct {
	// BucketName is the GCS bucket to open. Required.
	BucketName string
}

// systemClock is storage.Clock backed by the real wall clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// client is the gocloud.dev/blob-backed implementation of storage.Storage.
// It stays unexported so gocloud.dev types never escape the package.
type client struct {
	bucket      *blob.Bucket
	policies    map[string]storage.KindPolicy
	clock       storage.Clock
	uploadTTL   time.Duration
	downloadTTL time.Duration
}

var _ storage.Storage = (*client)(nil)

// NewClient dials a real Google Cloud Storage bucket using Application
// Default Credentials and returns a storage.Storage backed by it. The
// returned Storage owns the dialed connection; Close releases it.
func NewClient(ctx context.Context, cfg Config, opts storage.Options) (storage.Storage, error) { // coverage-ignore
	if cfg.BucketName == "" {
		return nil, fmt.Errorf("%w: BucketName must not be empty", storage.ErrInvalidConfig)
	}
	creds, err := gcp.DefaultCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs: resolving default credentials: %w", err)
	}
	httpClient, err := gcp.NewHTTPClient(http.DefaultTransport, gcp.CredentialsTokenSource(creds))
	if err != nil {
		return nil, fmt.Errorf("gcs: building HTTP client: %w", err)
	}
	bucket, err := gcsblob.OpenBucket(ctx, httpClient, cfg.BucketName, nil)
	if err != nil {
		return nil, fmt.Errorf("gcs: opening bucket %q: %w", cfg.BucketName, err)
	}
	s, err := NewFromBucket(bucket, opts)
	if err != nil {
		_ = bucket.Close()
		return nil, err
	}
	return s, nil
}

// NewFromBucket builds a storage.Storage over an already-opened *blob.Bucket
// — any gocloud.dev driver, not just gcsblob. This is the seam tests use to
// exercise the full policy/ref/sniff logic against blob/memblob or
// blob/fileblob without live GCS credentials; NewClient calls it internally
// for the production path.
func NewFromBucket(bucket *blob.Bucket, opts storage.Options) (storage.Storage, error) {
	if bucket == nil {
		return nil, fmt.Errorf("%w: bucket must not be nil", storage.ErrInvalidConfig)
	}
	policies, err := storage.ValidateKinds(opts.Kinds)
	if err != nil {
		return nil, err
	}
	clock := opts.Clock
	if clock == nil {
		clock = systemClock{}
	}
	uploadTTL := opts.UploadURLTTL
	if uploadTTL <= 0 {
		uploadTTL = defaultUploadTTL
	}
	downloadTTL := opts.DownloadURLTTL
	if downloadTTL <= 0 {
		downloadTTL = defaultDownloadTTL
	}
	return &client{
		bucket:      bucket,
		policies:    policies,
		clock:       clock,
		uploadTTL:   uploadTTL,
		downloadTTL: downloadTTL,
	}, nil
}

// CreateUploadIntent implements storage.Storage.
func (c *client) CreateUploadIntent(ctx context.Context, kind, contentType string, maxSize int64) (storage.UploadIntent, error) {
	p, ok := c.policies[kind]
	if !ok {
		return storage.UploadIntent{}, fmt.Errorf("%w: %q", storage.ErrUnknownKind, kind)
	}
	if !p.Allows(contentType) {
		return storage.UploadIntent{}, fmt.Errorf("%w: kind %q does not allow %q", storage.ErrContentTypeNotAllowed, kind, contentType)
	}

	// The per-intent maxSize may tighten the kind's cap, never raise it.
	effectiveMax := p.MaxSize
	if maxSize > 0 && maxSize < effectiveMax {
		effectiveMax = maxSize
	}

	key, ref, err := storage.MintRef(kind)
	if err != nil {
		return storage.UploadIntent{}, err
	}

	normalized := storage.NormalizeContentType(contentType)
	expiresAt := c.clock.Now().Add(c.uploadTTL)
	putURL, err := c.bucket.SignedURL(ctx, key, &blob.SignedURLOptions{
		Method:      http.MethodPut,
		Expiry:      c.uploadTTL,
		ContentType: normalized,
	})
	if err != nil {
		if gcerrors.Code(err) == gcerrors.Unimplemented {
			return storage.UploadIntent{}, fmt.Errorf("%w: PUT", storage.ErrSignedURLUnsupported)
		}
		return storage.UploadIntent{}, fmt.Errorf("gcs: signing PUT URL: %w", err)
	}

	return storage.UploadIntent{
		StorageRef:   ref,
		SignedPutURL: putURL,
		ContentType:  normalized,
		MaxSize:      effectiveMax,
		ExpiresAt:    expiresAt,
	}, nil
}

// ConfirmUpload implements storage.Storage.
func (c *client) ConfirmUpload(ctx context.Context, storageRef string) (storage.FileObject, error) {
	key, kind, err := storage.DecodeRef(storageRef)
	if err != nil {
		return storage.FileObject{}, err
	}
	p, ok := c.policies[kind]
	if !ok {
		return storage.FileObject{}, fmt.Errorf("%w: kind %q", storage.ErrInvalidRef, kind)
	}

	exists, err := c.bucket.Exists(ctx, key)
	if err != nil {
		return storage.FileObject{}, fmt.Errorf("gcs: checking object existence: %w", err)
	}
	if !exists {
		return storage.FileObject{}, fmt.Errorf("%w: %s", storage.ErrNotFound, storageRef)
	}

	attrs, err := c.bucket.Attributes(ctx, key)
	if err != nil {
		return storage.FileObject{}, fmt.Errorf("gcs: reading object attributes: %w", err)
	}
	if attrs.Size > p.MaxSize {
		return storage.FileObject{}, fmt.Errorf("%w: %d > %d", storage.ErrTooLarge, attrs.Size, p.MaxSize)
	}

	// Sniff the stored bytes: the client-declared Content-Type is not
	// trusted, since an executable can be uploaded under an image/png header.
	sniffed, err := c.sniff(ctx, key)
	if err != nil {
		return storage.FileObject{}, err
	}
	if !p.Allows(sniffed) {
		return storage.FileObject{}, fmt.Errorf("%w: sniffed %q for kind %q", storage.ErrContentTypeNotAllowed, sniffed, kind)
	}

	return storage.FileObject{
		StorageRef:  storageRef,
		Kind:        kind,
		ContentType: sniffed,
		Size:        attrs.Size,
	}, nil
}

// GetDownloadURL implements storage.Storage.
func (c *client) GetDownloadURL(ctx context.Context, storageRef string, ttl time.Duration) (string, error) {
	key, kind, err := storage.DecodeRef(storageRef)
	if err != nil {
		return "", err
	}
	if _, ok := c.policies[kind]; !ok {
		return "", fmt.Errorf("%w: kind %q", storage.ErrInvalidRef, kind)
	}
	exists, err := c.bucket.Exists(ctx, key)
	if err != nil {
		return "", fmt.Errorf("gcs: checking object existence: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("%w: %s", storage.ErrNotFound, storageRef)
	}
	if ttl <= 0 {
		ttl = c.downloadTTL
	}
	url, err := c.bucket.SignedURL(ctx, key, &blob.SignedURLOptions{
		Method: http.MethodGet,
		Expiry: ttl,
	})
	if err != nil {
		if gcerrors.Code(err) == gcerrors.Unimplemented {
			return "", fmt.Errorf("%w: GET", storage.ErrSignedURLUnsupported)
		}
		return "", fmt.Errorf("gcs: signing GET URL: %w", err)
	}
	return url, nil
}

// Download implements storage.Storage. The stored size is checked against
// the kind cap before any bytes are read, so a caller cannot be made to
// buffer an unbounded blob.
func (c *client) Download(ctx context.Context, storageRef string) ([]byte, error) {
	key, kind, err := storage.DecodeRef(storageRef)
	if err != nil {
		return nil, err
	}
	p, ok := c.policies[kind]
	if !ok {
		return nil, fmt.Errorf("%w: kind %q", storage.ErrInvalidRef, kind)
	}
	exists, err := c.bucket.Exists(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("gcs: checking object existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, storageRef)
	}
	attrs, err := c.bucket.Attributes(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("gcs: reading object attributes: %w", err)
	}
	if attrs.Size > p.MaxSize {
		return nil, fmt.Errorf("%w: %d > %d", storage.ErrTooLarge, attrs.Size, p.MaxSize)
	}
	data, err := c.bucket.ReadAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("gcs: reading object: %w", err)
	}
	return data, nil
}

// Close implements storage.Storage, releasing the underlying bucket.
func (c *client) Close() error {
	return c.bucket.Close()
}

// sniff reads up to sniffBytes from the object and detects its MIME type.
func (c *client) sniff(ctx context.Context, key string) (string, error) {
	r, err := c.bucket.NewRangeReader(ctx, key, 0, sniffBytes, nil)
	if err != nil {
		return "", fmt.Errorf("gcs: opening object for sniffing: %w", err)
	}
	defer func() { _ = r.Close() }()
	head, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("gcs: reading object for sniffing: %w", err)
	}
	return storage.NormalizeContentType(mimetype.Detect(head).String()), nil
}
