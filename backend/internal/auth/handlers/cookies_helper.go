package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// setCredentialCookies is the single place that emits the
// access / refresh / csrf trio. Used by Register, Login, Refresh,
// OIDCGoogleCallback. By centralizing the call we guarantee only those
// "credential events" set the csrf cookie (matches design.md D4 / spec).
func setCredentialCookies(c *gin.Context, d Deps, issued *service.IssuedTokens) {
	d.Cookies.SetAccess(c.Writer, issued.AccessToken)
	d.Cookies.SetRefresh(c.Writer, issued.RefreshToken)
	d.Cookies.SetCSRF(c.Writer, issued.CSRFToken)
}

// presentUser is the JSON shape returned for every "we just authenticated
// you" response and for /api/auth/me. Email + verification status is enough
// for the SPA's UI; we never expose internal fields.
func presentUser(u *models.User) gin.H {
	verified := u.EmailVerifiedAt != nil
	return gin.H{
		"id":             u.ID.String(),
		"email":          u.Email,
		"email_verified": verified,
		"created_at":     u.CreatedAt,
	}
}
