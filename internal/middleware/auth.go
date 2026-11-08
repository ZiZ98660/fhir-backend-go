package middleware

import (
	"context"
	"crypto/subtle"
	"go-fhir-phase1/internal"
	"net/http"
	"strings"
	"time"
)

// The token is supplied at startup; an empty token always fails closed.
// BEFORE (simplified excerpts; inactive):
// token := strings.TrimPrefix(authHeader, "Bearer ")
//
//	if token != SecurityConfig.SecretToken {
//	    sendFHIRError(w, http.StatusForbidden, "error", "Invalid or expired token")
//	    return
//	}
//
// next(w, r)
//
// NOW: receive the configured token as an argument, split/validate the header,
// compare the token, send 401 on failure, otherwise call next(w, r).
// The returned closure remembers secret and next. Checks run per request, not
// when this wrapper is constructed. An empty configured token fails closed.
// EqualFold makes the SCHEME case-insensitive, not the secret.
// ConstantTimeCompare avoids content-dependent timing for equal-length tokens;
// different-length inputs return immediately. This is still a shared token, not JWT.
// The || checks short-circuit: an invalid length prevents parts[0]/parts[1] access.
// WHY SHARED: wrap each protected handler once instead of repeating the check.
// Authentication already used middleware; this revision did not invent that layer.
func AuthMiddleware(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if secret == "" || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || subtle.ConstantTimeCompare([]byte(parts[1]), []byte(secret)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			internal.WriteOutcome(w, http.StatusUnauthorized, "login", "missing or invalid bearer token")
			return
		}
		next(w, r)
	}
}

// BEFORE: no application deadline wrapper; handlers received the original request.
// EARLIER FLOW (schematic, not an old function): next.ServeHTTP(w, r)
//
// NOW: create a child context with a 10-second deadline, attach it to a copy of
// the request, and pass that request onward. defer cancel releases its resources
// when handling returns. HTTP database calls use r.Context(), so they can stop.
// WHY A WRAPPER: all routes receive the policy without repeating it in handlers.
// Cancellation is cooperative: this does not kill Go code or auto-write a timeout.
// http.Handler is an interface with ServeHTTP; http.HandlerFunc adapts a function
// to that interface. It differs from AuthMiddleware's narrower function parameter.
func RequestTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
