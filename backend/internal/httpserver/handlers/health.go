// Package handlers contains the gin.HandlerFunc implementations exposed by the
// backend's HTTP server.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// healthPingTimeout bounds how long the /healthz handler waits on the database.
// We deliberately keep this short so liveness probes never hang.
const healthPingTimeout = 1 * time.Second

// Health returns a gin.HandlerFunc that reports both process liveness and
// database reachability. It returns:
//
//   - 200 OK with `{"status":"ok","db":"ok"}` when the database ping succeeds
//     within healthPingTimeout.
//   - 503 Service Unavailable with `{"status":"degraded","db":"down"}` when the
//     database is unreachable, the ping returns an error, or the timeout elapses.
func Health(gormDB *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sqlDB, err := gormDB.DB()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "degraded",
				"db":     "down",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), healthPingTimeout)
		defer cancel()

		if err := sqlDB.PingContext(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "degraded",
				"db":     "down",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"db":     "ok",
		})
	}
}
