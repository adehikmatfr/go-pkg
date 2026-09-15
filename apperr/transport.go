package apperr

import (
	"net/http"

	"google.golang.org/grpc/codes"
)

var httpStatusByCode = map[Code]int{
	CodeBadRequest:   http.StatusBadRequest,
	CodeUnauthorized: http.StatusUnauthorized,
	CodeForbidden:    http.StatusForbidden,
	CodeNotFound:     http.StatusNotFound,
	CodeConflict:     http.StatusConflict,
	CodeRateLimited:  http.StatusTooManyRequests,
	CodeInternal:     http.StatusInternalServerError,
}

var grpcCodeByCode = map[Code]codes.Code{
	CodeBadRequest:   codes.InvalidArgument,
	CodeUnauthorized: codes.Unauthenticated,
	CodeForbidden:    codes.PermissionDenied,
	CodeNotFound:     codes.NotFound,
	CodeConflict:     codes.AlreadyExists,
	CodeRateLimited:  codes.ResourceExhausted,
	CodeInternal:     codes.Internal,
}

// HTTPStatus returns the HTTP status code for code, defaulting to 500 for an
// unrecognized Code (including a zero value) so a boundary can always
// produce a valid response.
func HTTPStatus(code Code) int {
	if status, ok := httpStatusByCode[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// GRPCCode returns the gRPC status code for code, defaulting to
// codes.Internal for an unrecognized Code (including a zero value).
func GRPCCode(code Code) codes.Code {
	if c, ok := grpcCodeByCode[code]; ok {
		return c
	}
	return codes.Internal
}
