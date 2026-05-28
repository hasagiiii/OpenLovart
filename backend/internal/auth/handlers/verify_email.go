package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// VerifyEmail consumes the emailed token and flips users.email_verified_at.
// Accepts the token as `?token=` (preferred — the email link is a GET) but
// also as JSON body for SPA round-trips.
func VerifyEmail(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			var body struct {
				Token string `json:"token"`
			}
			_ = c.ShouldBindJSON(&body)
			token = body.Token
		}
		if token == "" {
			writeErr(c, http.StatusBadRequest, "missing_token", "")
			return
		}
		if err := d.Service.VerifyEmail(c.Request.Context(), token); err != nil {
			status, code := mapServiceError(err)
			writeErr(c, status, code, "")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
