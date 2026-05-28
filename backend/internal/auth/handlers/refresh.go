package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/refresh"
)

// Refresh rotates the refresh token (presented via the __Host-refresh
// cookie) and re-issues access + csrf cookies. Reuse triggers a 401 and
// chain-wide revocation handled inside the service.
func Refresh(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		plain, err := c.Cookie(d.Cookies.RefreshName())
		if err != nil || plain == "" {
			writeErr(c, http.StatusUnauthorized, "no_refresh", "")
			return
		}
		user, issued, err := d.Service.Refresh(
			c.Request.Context(),
			plain,
			c.Request.UserAgent(), c.ClientIP(),
		)
		if err != nil {
			if errors.Is(err, refresh.ErrReuseDetected) {
				d.Cookies.ClearAll(c.Writer)
				writeErr(c, http.StatusUnauthorized, "refresh_reused", "")
				return
			}
			if errors.Is(err, refresh.ErrUnknownToken) || errors.Is(err, refresh.ErrExpired) {
				d.Cookies.ClearAll(c.Writer)
				writeErr(c, http.StatusUnauthorized, "refresh_invalid", "")
				return
			}
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		setCredentialCookies(c, d, issued)
		c.JSON(http.StatusOK, gin.H{
			"ok":   true,
			"user": presentUser(user),
		})
	}
}
