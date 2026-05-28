package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type forgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email,max=320"`
}

// ForgotPassword always responds 200 to avoid email-enumeration. The
// service silently no-ops for unknown addresses.
func ForgotPassword(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req forgotPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeErr(c, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if err := d.Service.RequestPasswordReset(c.Request.Context(), req.Email); err != nil {
			// Internal failures are not surfaced to the client (consistent
			// with the anti-enumeration shape) but should be logged via the
			// access log middleware.
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
