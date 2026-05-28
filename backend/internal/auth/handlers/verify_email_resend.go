package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
)

// VerifyEmailResend regenerates the verification token and re-emails it.
// Authenticated endpoint: caller must already be signed in (the cookie/jwt
// asserts identity, the email itself just confirms it).
func VerifyEmailResend(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := middleware.CurrentUser(c)
		if !ok {
			writeErr(c, http.StatusUnauthorized, "unauthenticated", "")
			return
		}
		if err := d.Service.RequestEmailVerification(c.Request.Context(), user.ID); err != nil {
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
