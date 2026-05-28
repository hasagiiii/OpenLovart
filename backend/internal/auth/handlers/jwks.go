package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
)

// JWKS exposes the public-key JWK Set. Mounted unauthenticated and ahead
// of CSRF middleware in the router (see internal/httpserver/router.go).
func JWKS(ks *jwt.KeyStore) gin.HandlerFunc {
	h := jwt.Handler(ks)
	return func(c *gin.Context) {
		h(c.Writer, c.Request)
	}
}
