package gcs_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"

	"github.com/adehikmatfr/go-pkg/v2/datastore/storage"
	"github.com/adehikmatfr/go-pkg/v2/datastore/storage/gcs"
)

// pngMagic carries a real PNG signature so mimetype.Detect classifies it as
// image/png, exercising the same sniffing path production traffic hits.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

func newTestBucket(t *testing.T) *blob.Bucket {
	t.Helper()
	b := memblob.OpenBucket(nil)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func testOptions() storage.Options {
	return storage.Options{
		Kinds: []storage.KindPolicy{
			{Name: "avatar", AllowedContentTypes: []string{"image/png"}, MaxSize: 1 << 20},
			{Name: "kyc_document", AllowedContentTypes: []string{"application/pdf", "image/png"}, MaxSize: 5 << 20},
		},
		Clock: fixedClock{t: time.Unix(1_700_000_000, 0).UTC()},
	}
}

func TestNewFromBucket_invalidConfig(t *testing.T) {
	t.Run("nil bucket", func(t *testing.T) {
		_, err := gcs.NewFromBucket(nil, testOptions())
		if !errors.Is(err, storage.ErrInvalidConfig) {
			t.Fatalf("err = %v, want ErrInvalidConfig", err)
		}
	})

	t.Run("no kinds", func(t *testing.T) {
		_, err := gcs.NewFromBucket(newTestBucket(t), storage.Options{})
		if !errors.Is(err, storage.ErrInvalidConfig) {
			t.Fatalf("err = %v, want ErrInvalidConfig", err)
		}
	})
}

func TestNewClient_invalidConfig(t *testing.T) {
	_, err := gcs.NewClient(context.Background(), gcs.Config{}, testOptions())
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestCreateUploadIntent_policyGate(t *testing.T) {
	ctx := context.Background()
	s, err := gcs.NewFromBucket(newTestBucket(t), testOptions())
	if err != nil {
		t.Fatalf("NewFromBucket() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	t.Run("unknown kind", func(t *testing.T) {
		_, err := s.CreateUploadIntent(ctx, "nonexistent", "image/png", 0)
		if !errors.Is(err, storage.ErrUnknownKind) {
			t.Fatalf("err = %v, want ErrUnknownKind", err)
		}
	})

	t.Run("disallowed content type never reaches the signer", func(t *testing.T) {
		_, err := s.CreateUploadIntent(ctx, "avatar", "text/plain", 0)
		if !errors.Is(err, storage.ErrContentTypeNotAllowed) {
			t.Fatalf("err = %v, want ErrContentTypeNotAllowed", err)
		}
	})

	t.Run("allowed type passes the gate but memblob cannot sign", func(t *testing.T) {
		_, err := s.CreateUploadIntent(ctx, "avatar", "image/png", 0)
		if !errors.Is(err, storage.ErrSignedURLUnsupported) {
			t.Fatalf("err = %v, want ErrSignedURLUnsupported", err)
		}
	})
}

func TestConfirmUpload(t *testing.T) {
	ctx := context.Background()
	bucket := newTestBucket(t)
	s, err := gcs.NewFromBucket(bucket, testOptions())
	if err != nil {
		t.Fatalf("NewFromBucket() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	t.Run("valid png", func(t *testing.T) {
		key, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		if writeErr := bucket.WriteAll(ctx, key, pngMagic, nil); writeErr != nil {
			t.Fatalf("WriteAll() error = %v", writeErr)
		}
		obj, err := s.ConfirmUpload(ctx, ref)
		if err != nil {
			t.Fatalf("ConfirmUpload() error = %v", err)
		}
		if obj.ContentType != "image/png" {
			t.Errorf("ContentType = %q, want image/png", obj.ContentType)
		}
		if obj.Kind != "avatar" {
			t.Errorf("Kind = %q, want avatar", obj.Kind)
		}
		if obj.Size != int64(len(pngMagic)) {
			t.Errorf("Size = %d, want %d", obj.Size, len(pngMagic))
		}
	})

	t.Run("lying content type rejected by sniffing", func(t *testing.T) {
		key, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		if writeErr := bucket.WriteAll(ctx, key, []byte("plain text, not a png"), nil); writeErr != nil {
			t.Fatalf("WriteAll() error = %v", writeErr)
		}
		_, err := s.ConfirmUpload(ctx, ref)
		if !errors.Is(err, storage.ErrContentTypeNotAllowed) {
			t.Fatalf("err = %v, want ErrContentTypeNotAllowed", err)
		}
	})

	t.Run("oversize rejected", func(t *testing.T) {
		key, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		oversized := append([]byte{}, pngMagic...)
		oversized = append(oversized, bytes.Repeat([]byte{0}, 2<<20)...)
		if writeErr := bucket.WriteAll(ctx, key, oversized, nil); writeErr != nil {
			t.Fatalf("WriteAll() error = %v", writeErr)
		}
		_, err := s.ConfirmUpload(ctx, ref)
		if !errors.Is(err, storage.ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		_, err := s.ConfirmUpload(ctx, ref)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid ref", func(t *testing.T) {
		_, err := s.ConfirmUpload(ctx, "not-a-valid-ref!!")
		if !errors.Is(err, storage.ErrInvalidRef) {
			t.Fatalf("err = %v, want ErrInvalidRef", err)
		}
	})

	t.Run("unknown kind in ref", func(t *testing.T) {
		_, ref, mintErr := storage.MintRef("nonexistent_kind")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		_, err := s.ConfirmUpload(ctx, ref)
		if !errors.Is(err, storage.ErrInvalidRef) {
			t.Fatalf("err = %v, want ErrInvalidRef", err)
		}
	})
}

func TestGetDownloadURL(t *testing.T) {
	ctx := context.Background()
	bucket := newTestBucket(t)
	s, err := gcs.NewFromBucket(bucket, testOptions())
	if err != nil {
		t.Fatalf("NewFromBucket() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	key, ref, mintErr := storage.MintRef("avatar")
	if mintErr != nil {
		t.Fatalf("MintRef() error = %v", mintErr)
	}
	if writeErr := bucket.WriteAll(ctx, key, pngMagic, nil); writeErr != nil {
		t.Fatalf("WriteAll() error = %v", writeErr)
	}

	t.Run("memblob cannot sign", func(t *testing.T) {
		_, err := s.GetDownloadURL(ctx, ref, 0)
		if !errors.Is(err, storage.ErrSignedURLUnsupported) {
			t.Fatalf("err = %v, want ErrSignedURLUnsupported", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, missingRef, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		_, err := s.GetDownloadURL(ctx, missingRef, 0)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid ref", func(t *testing.T) {
		_, err := s.GetDownloadURL(ctx, "garbage", 0)
		if !errors.Is(err, storage.ErrInvalidRef) {
			t.Fatalf("err = %v, want ErrInvalidRef", err)
		}
	})
}

func TestDownload(t *testing.T) {
	ctx := context.Background()
	bucket := newTestBucket(t)
	s, err := gcs.NewFromBucket(bucket, testOptions())
	if err != nil {
		t.Fatalf("NewFromBucket() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	t.Run("returns stored bytes", func(t *testing.T) {
		key, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		if writeErr := bucket.WriteAll(ctx, key, pngMagic, nil); writeErr != nil {
			t.Fatalf("WriteAll() error = %v", writeErr)
		}
		got, err := s.Download(ctx, ref)
		if err != nil {
			t.Fatalf("Download() error = %v", err)
		}
		if !bytes.Equal(got, pngMagic) {
			t.Errorf("Download() = %v, want %v", got, pngMagic)
		}
	})

	t.Run("oversize rejected before reading", func(t *testing.T) {
		key, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		oversized := bytes.Repeat([]byte{0}, 2<<20)
		if writeErr := bucket.WriteAll(ctx, key, oversized, nil); writeErr != nil {
			t.Fatalf("WriteAll() error = %v", writeErr)
		}
		_, err := s.Download(ctx, ref)
		if !errors.Is(err, storage.ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, ref, mintErr := storage.MintRef("avatar")
		if mintErr != nil {
			t.Fatalf("MintRef() error = %v", mintErr)
		}
		_, err := s.Download(ctx, ref)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid ref", func(t *testing.T) {
		_, err := s.Download(ctx, "garbage")
		if !errors.Is(err, storage.ErrInvalidRef) {
			t.Fatalf("err = %v, want ErrInvalidRef", err)
		}
	})
}

func TestClose(t *testing.T) {
	s, err := gcs.NewFromBucket(newTestBucket(t), testOptions())
	if err != nil {
		t.Fatalf("NewFromBucket() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
