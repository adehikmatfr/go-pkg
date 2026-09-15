// Package apperr is a small error taxonomy for classifying errors at a
// service boundary (HTTP handler, gRPC interceptor) without changing how
// domain/usecase code returns errors.
//
// Domain code keeps returning plain sentinel errors (errors.New, fmt.Errorf
// with %w). A Registry built at the boundary maps those sentinels to a
// stable Code; Resolve never requires the caller to change an existing
// function's return type or error values.
package apperr

import "strings"

// Code is a stable, machine-readable error category.
type Code string

const (
	// CodeInternal is the fail-closed default for any error not registered
	// in a Registry.
	CodeInternal Code = "INTERNAL"
	// CodeBadRequest means the request is malformed or fails validation.
	CodeBadRequest Code = "BAD_REQUEST"
	// CodeUnauthorized means the caller has no valid credentials.
	CodeUnauthorized Code = "UNAUTHORIZED"
	// CodeForbidden means the caller is authenticated but lacks permission.
	CodeForbidden Code = "FORBIDDEN"
	// CodeNotFound means the requested resource does not exist.
	CodeNotFound Code = "NOT_FOUND"
	// CodeConflict means the request conflicts with the current state.
	CodeConflict Code = "CONFLICT"
	// CodeRateLimited means the caller exceeded a rate or attempt budget.
	CodeRateLimited Code = "RATE_LIMITED"
)

// KeyFor returns the i18n message key convention for code, e.g.
// CodeNotFound -> "error.not_found". apperr never renders a message itself;
// a caller who wants localized copy passes this key into its own translator
// (e.g. github.com/adehikmatfr/go-pkg/v2/i18n's Bundle).
func KeyFor(code Code) string {
	return "error." + strings.ToLower(string(code))
}
