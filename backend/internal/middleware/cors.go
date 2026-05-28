package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS emits the standard cross-origin headers when origin is non-empty.
// We never echo `Access-Control-Allow-Origin: *` — credentials are required
// (cookies) so origin must be a single, exact value. When origin is the
// empty string the middleware is effectively a no-op (used in dev where
// the Next.js rewrite proxy makes the browser see same-origin traffic).
func CORS(origin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if origin == "" {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-Id")
		c.Header("Access-Control-Max-Age", "600")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
