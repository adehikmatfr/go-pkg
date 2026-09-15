package apperr_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"

	"github.com/adehikmatfr/go-pkg/v2/apperr"
)

func TestKeyFor(t *testing.T) {
	tests := []struct {
		name string
		code apperr.Code
		want string
	}{
		{name: "not found", code: apperr.CodeNotFound, want: "error.not_found"},
		{name: "internal", code: apperr.CodeInternal, want: "error.internal"},
		{name: "bad request", code: apperr.CodeBadRequest, want: "error.bad_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apperr.KeyFor(tt.code); got != tt.want {
				t.Errorf("KeyFor(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestRegistry_Resolve(t *testing.T) {
	errUserNotFound := errors.New("user not found")
	errDuplicateEmail := errors.New("duplicate email")

	reg := apperr.NewRegistry().
		Register(errUserNotFound, apperr.CodeNotFound).
		RegisterFunc(errDuplicateEmail, apperr.CodeConflict, func(err error) map[string]any {
			return map[string]any{"Field": "email"}
		})

	tests := []struct {
		name     string
		err      error
		wantCode apperr.Code
		wantKey  string
		wantArgs map[string]any
	}{
		{
			name:     "registered sentinel",
			err:      errUserNotFound,
			wantCode: apperr.CodeNotFound,
			wantKey:  "error.not_found",
		},
		{
			name:     "fmt.Errorf-wrapped registered sentinel",
			err:      fmt.Errorf("lookup: %w", errUserNotFound),
			wantCode: apperr.CodeNotFound,
			wantKey:  "error.not_found",
		},
		{
			name:     "similar message but not the same sentinel",
			err:      errors.New("user not found"),
			wantCode: apperr.CodeInternal,
			wantKey:  "error.internal",
		},
		{
			name:     "registered sentinel with derived args",
			err:      errDuplicateEmail,
			wantCode: apperr.CodeConflict,
			wantKey:  "error.conflict",
			wantArgs: map[string]any{"Field": "email"},
		},
		{
			name:     "unregistered error fails closed",
			err:      errors.New("boom"),
			wantCode: apperr.CodeInternal,
			wantKey:  "error.internal",
		},
		{
			name:     "nil error fails closed",
			err:      nil,
			wantCode: apperr.CodeInternal,
			wantKey:  "error.internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.Resolve(tt.err)
			if got.Code != tt.wantCode {
				t.Errorf("Resolve().Code = %q, want %q", got.Code, tt.wantCode)
			}
			if got.Key != tt.wantKey {
				t.Errorf("Resolve().Key = %q, want %q", got.Key, tt.wantKey)
			}
			if len(got.Args) != len(tt.wantArgs) {
				t.Fatalf("Resolve().Args = %v, want %v", got.Args, tt.wantArgs)
			}
			for k, v := range tt.wantArgs {
				if got.Args[k] != v {
					t.Errorf("Resolve().Args[%q] = %v, want %v", k, got.Args[k], v)
				}
			}
		})
	}
}

func TestRegistry_Resolve_wrappedSentinel(t *testing.T) {
	errUserNotFound := errors.New("user not found")
	wrapped := errors.Join(errors.New("context"), errUserNotFound)

	reg := apperr.NewRegistry().Register(errUserNotFound, apperr.CodeNotFound)

	got := reg.Resolve(wrapped)
	if got.Code != apperr.CodeNotFound {
		t.Errorf("Resolve(wrapped).Code = %q, want %q", got.Code, apperr.CodeNotFound)
	}
}

func TestRegistry_Register_nilSentinelIgnored(t *testing.T) {
	reg := apperr.NewRegistry().Register(nil, apperr.CodeNotFound)

	got := reg.Resolve(errors.New("anything"))
	if got.Code != apperr.CodeInternal {
		t.Errorf("Resolve() with nil-sentinel registration = %q, want %q", got.Code, apperr.CodeInternal)
	}
}

func TestRegistry_orderMattersFirstMatchWins(t *testing.T) {
	base := errors.New("base")
	wrapped := errors.Join(base)

	reg := apperr.NewRegistry().
		Register(wrapped, apperr.CodeConflict).
		Register(base, apperr.CodeNotFound)

	got := reg.Resolve(wrapped)
	if got.Code != apperr.CodeConflict {
		t.Errorf("Resolve() = %q, want first registered match %q", got.Code, apperr.CodeConflict)
	}
}

func TestHTTPStatus(t *testing.T) {
	tests := []struct {
		code apperr.Code
		want int
	}{
		{apperr.CodeBadRequest, http.StatusBadRequest},
		{apperr.CodeUnauthorized, http.StatusUnauthorized},
		{apperr.CodeForbidden, http.StatusForbidden},
		{apperr.CodeNotFound, http.StatusNotFound},
		{apperr.CodeConflict, http.StatusConflict},
		{apperr.CodeRateLimited, http.StatusTooManyRequests},
		{apperr.CodeInternal, http.StatusInternalServerError},
		{apperr.Code("UNKNOWN"), http.StatusInternalServerError},
		{"", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if got := apperr.HTTPStatus(tt.code); got != tt.want {
				t.Errorf("HTTPStatus(%q) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestGRPCCode(t *testing.T) {
	tests := []struct {
		code apperr.Code
		want codes.Code
	}{
		{apperr.CodeBadRequest, codes.InvalidArgument},
		{apperr.CodeUnauthorized, codes.Unauthenticated},
		{apperr.CodeForbidden, codes.PermissionDenied},
		{apperr.CodeNotFound, codes.NotFound},
		{apperr.CodeConflict, codes.AlreadyExists},
		{apperr.CodeRateLimited, codes.ResourceExhausted},
		{apperr.CodeInternal, codes.Internal},
		{apperr.Code("UNKNOWN"), codes.Internal},
		{"", codes.Internal},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if got := apperr.GRPCCode(tt.code); got != tt.want {
				t.Errorf("GRPCCode(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}
