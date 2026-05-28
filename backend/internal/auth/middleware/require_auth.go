// Package middleware holds auth-aware Gin middleware used by both auth
// endpoints and downstream business APIs. The package intentionally lives
// under auth/ (not the project-wide internal/middleware/) so it can import
// the auth/jwt and auth/cookies subpackages without cycles.
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// contextKey is the Gin context key for the authenticated user. We use a
// typed wrapper so a future audit (`grep currentUserKey`) reliably finds
// every read site.
const currentUserKey = "currentUser"

// CurrentUser extracts the authenticated user attached by RequireAuth.
// Returns ok=false when the middleware did not run or no user was found.
func CurrentUser(c *gin.Context) (*models.User, bool) {
	v, ok := c.Get(currentUserKey)
	if !ok {
		return nil, false
	}
	u, ok := v.(*models.User)
	return u, ok
}

// RequireAuth is the gate every authenticated route lives behind. It looks
// for an access token in (a) the configured access cookie, falling back to
// (b) the `Authorization: Bearer <token>` header. The kid header on the
// JWT picks the verification key out of the supplied KeyStore.
//
// On success the looked-up *models.User is attached to the Gin context;
// downstream handlers retrieve it via CurrentUser. On failure we respond
// 401 with the canonical {error:"unauthenticated"} envelope.
func RequireAuth(db *gorm.DB, ks *jwt.KeyStore, ck *cookies.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c, ck)
		if token == "" {
			abort401(c)
			return
		}
		claims, err := jwt.Verify(ks, token)
		if err != nil {
			abort401(c)
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			abort401(c)
			return
		}
		var user models.User
		if err := db.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				abort401(c)
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
			return
		}
		c.Set(currentUserKey, &user)
		c.Next()
	}
}

func extractToken(c *gin.Context, ck *cookies.Manager) string {
	if v, err := c.Cookie(ck.AccessName()); err == nil && v != "" {
		return v
	}
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func abort401(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
}
