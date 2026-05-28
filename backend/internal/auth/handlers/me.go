package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/middleware"
)

// Me returns the currently-authenticated user. Per spec D4, this handler
// does NOT set or rotate the csrf cookie — issuance is reserved for
// credential-issuing endpoints (register / login / refresh / OIDC callback).
func Me(_ Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := middleware.CurrentUser(c)
		if !ok {
			writeErr(c, http.StatusUnauthorized, "unauthenticated", "")
			return
		}
		c.JSON(http.StatusOK, gin.H{"user": presentUser(user)})
	}
}
