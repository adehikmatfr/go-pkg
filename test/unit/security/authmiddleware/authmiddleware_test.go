package authmiddleware_test

import (
	"context"
	"errors"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/security/authmiddleware"
	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    string
		wantErr error
	}{
		{name: "valid", header: "Bearer abc.def.ghi", want: "abc.def.ghi"},
		{name: "empty header", header: "", wantErr: authmiddleware.ErrMissingToken},
		{name: "wrong scheme", header: "Basic abc", wantErr: authmiddleware.ErrMissingToken},
		{name: "empty token after prefix", header: "Bearer ", wantErr: authmiddleware.ErrMissingToken},
		{name: "only whitespace token", header: "Bearer    ", wantErr: authmiddleware.ErrMissingToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := authmiddleware.BearerToken(tt.header)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("BearerToken(%q) error = %v, want %v", tt.header, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BearerToken(%q) unexpected error: %v", tt.header, err)
			}
			if got != tt.want {
				t.Errorf("BearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestClaimsFromContextMissing(t *testing.T) {
	if _, ok := authmiddleware.ClaimsFromContext(context.Background()); ok {
		t.Error("ClaimsFromContext() on a bare context should report ok=false")
	}
}

func TestWithClaimsAndClaimsFromContext(t *testing.T) {
	want := &jwt.Claims{Role: "admin"}
	ctx := authmiddleware.WithClaims(context.Background(), want)

	got, ok := authmiddleware.ClaimsFromContext(ctx)
	if !ok {
		t.Fatal("ClaimsFromContext() reported ok=false, want true")
	}
	if got != want {
		t.Errorf("ClaimsFromContext() = %p, want the same pointer %p", got, want)
	}
}
