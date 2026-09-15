package storage_test

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/datastore/storage"
)

func base64Encode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestValidateKinds(t *testing.T) {
	tests := []struct {
		name    string
		kinds   []storage.KindPolicy
		wantErr bool
	}{
		{
			name: "valid single kind",
			kinds: []storage.KindPolicy{
				{Name: "avatar", AllowedContentTypes: []string{"image/png"}, MaxSize: 1024},
			},
		},
		{
			name: "valid multiple kinds",
			kinds: []storage.KindPolicy{
				{Name: "avatar", AllowedContentTypes: []string{"image/png"}, MaxSize: 1024},
				{Name: "kyc_document", AllowedContentTypes: []string{"application/pdf"}, MaxSize: 5 << 20},
			},
		},
		{name: "no kinds", kinds: nil, wantErr: true},
		{
			name:    "empty name",
			kinds:   []storage.KindPolicy{{Name: "", AllowedContentTypes: []string{"image/png"}, MaxSize: 1}},
			wantErr: true,
		},
		{
			name: "duplicate name",
			kinds: []storage.KindPolicy{
				{Name: "avatar", AllowedContentTypes: []string{"image/png"}, MaxSize: 1},
				{Name: "avatar", AllowedContentTypes: []string{"image/jpeg"}, MaxSize: 1},
			},
			wantErr: true,
		},
		{
			name:    "no allowed content types",
			kinds:   []storage.KindPolicy{{Name: "avatar", MaxSize: 1}},
			wantErr: true,
		},
		{
			name:    "zero max size",
			kinds:   []storage.KindPolicy{{Name: "avatar", AllowedContentTypes: []string{"image/png"}, MaxSize: 0}},
			wantErr: true,
		},
		{
			name:    "negative max size",
			kinds:   []storage.KindPolicy{{Name: "avatar", AllowedContentTypes: []string{"image/png"}, MaxSize: -1}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policies, err := storage.ValidateKinds(tt.kinds)
			if tt.wantErr {
				if !errors.Is(err, storage.ErrInvalidConfig) {
					t.Fatalf("ValidateKinds() err = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(policies) != len(tt.kinds) {
				t.Fatalf("ValidateKinds() returned %d policies, want %d", len(policies), len(tt.kinds))
			}
			for _, k := range tt.kinds {
				if policies[k.Name].Name != k.Name {
					t.Errorf("policies[%q] = %+v, want Name %q", k.Name, policies[k.Name], k.Name)
				}
			}
		})
	}
}

func TestNormalizeContentType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "already normalized", in: "image/png", want: "image/png"},
		{name: "uppercase", in: "Image/PNG", want: "image/png"},
		{name: "with parameters", in: "text/html; charset=utf-8", want: "text/html"},
		{name: "with surrounding space", in: "  image/png  ", want: "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := storage.NormalizeContentType(tt.in); got != tt.want {
				t.Errorf("NormalizeContentType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestKindPolicy_Allows(t *testing.T) {
	tests := []struct {
		name        string
		policy      storage.KindPolicy
		contentType string
		want        bool
	}{
		{
			name:        "exact match",
			policy:      storage.KindPolicy{AllowedContentTypes: []string{"image/png", "image/jpeg"}},
			contentType: "image/png",
			want:        true,
		},
		{
			name:        "case-insensitive match",
			policy:      storage.KindPolicy{AllowedContentTypes: []string{"image/png"}},
			contentType: "Image/PNG",
			want:        true,
		},
		{
			name:        "not allowed",
			policy:      storage.KindPolicy{AllowedContentTypes: []string{"image/png"}},
			contentType: "application/pdf",
			want:        false,
		},
		{
			name:        "wildcard allows any",
			policy:      storage.KindPolicy{AllowedContentTypes: []string{"*/*"}},
			contentType: "application/x-executable",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Allows(tt.contentType); got != tt.want {
				t.Errorf("Allows(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestMintRefAndDecodeRef_roundTrip(t *testing.T) {
	key, ref, err := storage.MintRef("kyc_document")
	if err != nil {
		t.Fatalf("MintRef() error = %v", err)
	}
	if key == "" || ref == "" {
		t.Fatalf("MintRef() returned empty key/ref: key=%q ref=%q", key, ref)
	}

	decodedKey, decodedKind, err := storage.DecodeRef(ref)
	if err != nil {
		t.Fatalf("DecodeRef() error = %v", err)
	}
	if decodedKey != key {
		t.Errorf("DecodeRef() key = %q, want %q", decodedKey, key)
	}
	if decodedKind != "kyc_document" {
		t.Errorf("DecodeRef() kind = %q, want %q", decodedKind, "kyc_document")
	}
}

func TestMintRef_unguessable(t *testing.T) {
	seen := make(map[string]bool)
	const n = 200
	for i := 0; i < n; i++ {
		_, ref, err := storage.MintRef("avatar")
		if err != nil {
			t.Fatalf("MintRef() error = %v", err)
		}
		if seen[ref] {
			t.Fatalf("MintRef() produced a duplicate ref after %d mints", i)
		}
		seen[ref] = true
	}
}

func TestDecodeRef_invalid(t *testing.T) {
	validRef := func() string {
		_, ref, err := storage.MintRef("avatar")
		if err != nil {
			t.Fatalf("MintRef() error = %v", err)
		}
		return ref
	}

	tests := []struct {
		name string
		ref  string
	}{
		{name: "empty", ref: ""},
		{name: "not base64", ref: "not base64!!"},
		{name: "missing separator", ref: "YXZhdGFyLXRva2Vu"}, // base64("avatar-token"), no ':'
		{name: "path traversal in kind", ref: base64Encode("../etc:token")},
		{name: "path traversal in token", ref: base64Encode("avatar:../../secret")},
		{name: "empty kind", ref: base64Encode(":token")},
		{name: "empty token", ref: base64Encode("avatar:")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := storage.DecodeRef(tt.ref)
			if !errors.Is(err, storage.ErrInvalidRef) {
				t.Fatalf("DecodeRef(%q) err = %v, want ErrInvalidRef", tt.ref, err)
			}
		})
	}

	// Sanity: a well-formed ref from MintRef must not be rejected.
	if _, _, err := storage.DecodeRef(validRef()); err != nil {
		t.Fatalf("DecodeRef() on a MintRef-produced ref returned error: %v", err)
	}
}
