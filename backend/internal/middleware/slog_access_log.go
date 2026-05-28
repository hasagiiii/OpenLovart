package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	authmw "github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
)

// SlogAccessLog replaces Gin's default text logger with a slog-based access
// log. Every request emits exactly one INFO line at completion containing
// method, path, status, latency, request id, client ip, and (if present)
// the authenticated user id.
//
// Mount this BEFORE RequireAuth on the route stack so the middleware sees
// the user attached by RequireAuth via auth/middleware.CurrentUser.
func SlogAccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.Request.URL.Path
		if c.Request.URL.RawQuery != "" {
			path = path + "?" + c.Request.URL.RawQuery
		}

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", time.Since(start)),
			slog.String("client_ip", c.ClientIP()),
		}
		if rid, ok := c.Get("request_id"); ok {
			if s, ok := rid.(string); ok && s != "" {
				attrs = append(attrs, slog.String("request_id", s))
			}
		}
		if user, ok := authmw.CurrentUser(c); ok && user != nil {
			attrs = append(attrs, slog.String("user_id", user.ID.String()))
		}
		log.LogAttrs(c.Request.Context(), slog.LevelInfo, "http request", attrs...)
	}
}
