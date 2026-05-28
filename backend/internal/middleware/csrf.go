// Package middleware holds Gin middleware that is not auth-specific in the
// "extract the user" sense — CSRF, CORS, request-id, access logging, and
// the per-route rate-limit factory. The user-extraction middleware lives in
// internal/auth/middleware/require_auth.go to keep the auth package
// boundary clean.
package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
)

// CSRFConfig is the small configuration block consumed by CSRF.
type CSRFConfig struct {
	// Cookies provides the wire-level access-cookie name (so we can detect
	// "is this a cookie-authenticated request?") and the CSRF cookie name
	// (so we can read the expected value).
	Cookies *cookies.Manager
}

// safeMethods are the HTTP methods exempt from CSRF enforcement. RFC 9110
// classifies these as "safe" / read-only.
var safeMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodOptions: {},
}

// CSRF enforces the double-submit token pattern described in design.md D4.
//
// Rules:
//   - GET / HEAD / OPTIONS pass through unchanged.
//   - Requests carrying an `Authorization: Bearer …` header pass through
//     (machine-to-machine traffic doesn't ride a cookie, so CSRF is N/A).
//   - Requests WITHOUT the access cookie pass through (they cannot mutate
//     authenticated state without ALSO failing RequireAuth downstream).
//   - Otherwise the X-CSRF-Token header MUST equal the `csrf` cookie value
//     in constant time.
//
// This middleware MUST NOT issue or rotate the csrf cookie itself; that is
// the responsibility of credential-issuing handlers (register / login /
// refresh / oidc callback). See design.md / spec.md for the rationale.
func CSRF(cfg CSRFConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := safeMethods[c.Request.Method]; ok {
			c.Next()
			return
		}
		// Bearer-authed traffic skips CSRF.
		if hdr := c.GetHeader("Authorization"); hdr != "" {
			c.Next()
			return
		}
		// Only enforce when the access cookie is actually present, i.e. the
		// request is cookie-authenticated and could plausibly be a CSRF.
		accessCookie, err := c.Cookie(cfg.Cookies.AccessName())
		if err != nil || accessCookie == "" {
			c.Next()
			return
		}

		cookieVal, err := c.Cookie(cfg.Cookies.CSRFName())
		header := c.GetHeader("X-CSRF-Token")
		if err != nil || cookieVal == "" || header == "" || len(cookieVal) != len(header) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "csrf"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(cookieVal), []byte(header)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "csrf"})
			return
		}
		c.Next()
	}
}
