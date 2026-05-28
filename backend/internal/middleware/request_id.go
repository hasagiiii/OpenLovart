package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// requestIDContextKey is the context.Context key under which the request id
// is stashed for non-Gin consumers (e.g. service-layer code that wants to
// log it).
type requestIDContextKey struct{}

// RequestIDHeader is the canonical header name for the request id, in both
// directions (client → server, server → client).
const RequestIDHeader = "X-Request-Id"

// RequestID reads X-Request-Id from the request or generates a fresh UUID,
// echoes it on the response, and stores it on both gin.Context (key
// "request_id") and the underlying context.Context (typed key) so service
// code can pick it up via RequestIDFromContext.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(RequestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Set("request_id", rid)
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), requestIDContextKey{}, rid),
		)
		c.Writer.Header().Set(RequestIDHeader, rid)
		c.Next()
	}
}

// RequestIDFromContext returns the request id stored on the context, or "".
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return v
	}
	return ""
}
