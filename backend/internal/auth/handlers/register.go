package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
)

type registerRequest struct {
	Email    string `json:"email" binding:"required,email,max=320"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

// Register issues credentials for a brand-new user. To prevent email
// enumeration the handler responds with 200 even when the email is already
// taken — the service may queue an "account already exists" notification
// instead, but the wire-level shape is constant.
func Register(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeErr(c, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		user, issued, err := d.Service.Register(
			c.Request.Context(),
			req.Email, req.Password,
			c.Request.UserAgent(), c.ClientIP(),
		)
		if err != nil {
			if errors.Is(err, service.ErrEmailAlreadyRegistered) {
				// Anti-enumeration: pretend success.
				c.JSON(http.StatusOK, gin.H{"ok": true})
				return
			}
			status, code := mapServiceError(err)
			writeErr(c, status, code, "")
			return
		}
		setCredentialCookies(c, d, issued)
		c.JSON(http.StatusOK, gin.H{
			"ok":   true,
			"user": presentUser(user),
		})
	}
}
