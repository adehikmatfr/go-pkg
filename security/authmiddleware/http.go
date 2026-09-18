package authmiddleware

import (
	"net/http"

	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

// HTTP returns net/http middleware that validates the request's bearer
// token using signer and injects its Claims into the request context via
// WithClaims before calling next. A missing or invalid token short-circuits
// with 401 Unauthorized and never calls next.
func HTTP(signer jwt.Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := BearerToken(r.Header.Get("Authorization"))
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims, err := signer.Parse(token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}
