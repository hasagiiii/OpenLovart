package middleware

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/ratelimit"
)

// KeyExtractor pulls the rate-limit key from a request. Common choices:
//   - per-IP:   func(c *gin.Context) string { return c.ClientIP() }
//   - per-user: func(c *gin.Context) string { return userIDFromContext(c) }
//   - per-form: e.g. extracting the email field from a JSON body
//
// Returning the empty string short-circuits the limiter (i.e. the request
// is allowed) so callers can opt out for unauthenticated probes.
type KeyExtractor func(c *gin.Context) string

// PerIP is the canonical extractor for unauthenticated routes.
func PerIP(c *gin.Context) string { return c.ClientIP() }

// RateLimit returns Gin middleware that consults the supplied limiter using
// the supplied KeyExtractor. On deny it sets `Retry-After` (seconds, rounded
// up) and responds 429 with `{error:"rate_limited"}`.
func RateLimit(limiter ratelimit.Limiter, extract KeyExtractor) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := extract(c)
		if key == "" {
			c.Next()
			return
		}
		ok, retry := limiter.Allow(c.Request.Context(), key)
		if !ok {
			seconds := int(math.Ceil(retry.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", fmt.Sprintf("%d", seconds))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate_limited"})
			return
		}
		c.Next()
	}
}

// Now is exported for testability of duration-formatting code.
var Now = time.Now
