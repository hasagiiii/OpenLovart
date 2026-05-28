package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type resetPasswordRequest struct {
	Token       string `json:"token" binding:"required,min=10,max=512"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

// ResetPassword consumes the emailed token and sets a new password. After
// success every refresh token for the user has been revoked by the
// service, so the client must log in again.
func ResetPassword(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req resetPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeErr(c, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if err := d.Service.ResetPassword(c.Request.Context(), req.Token, req.NewPassword); err != nil {
			status, code := mapServiceError(err)
			writeErr(c, status, code, "")
			return
		}
		// Defensive: the service revoked all sessions; clear cookies if any
		// were attached (e.g. the user was signed in elsewhere).
		d.Cookies.ClearAll(c.Writer)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
