package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Logout best-effort revokes the presented refresh token and clears every
// auth cookie. Always returns 200 so SPAs can navigate away unconditionally.
func Logout(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if plain, err := c.Cookie(d.Cookies.RefreshName()); err == nil && plain != "" {
			_ = d.Service.Logout(c.Request.Context(), plain)
		}
		d.Cookies.ClearAll(c.Writer)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
