package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// OIDCGoogleCallback completes the Google OIDC handshake. On success it
// sets the credential cookies and 302s to the original "next" URL (or "/"
// if absent / unsafe).
func OIDCGoogleCallback(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		code := c.Query("code")
		state := c.Query("state")
		if code == "" || state == "" {
			writeErr(c, http.StatusBadRequest, "invalid_oidc_response", "")
			return
		}
		signed, err := c.Cookie(d.Cookies.OIDCStateName())
		if err != nil || signed == "" {
			writeErr(c, http.StatusBadRequest, "missing_state", "")
			return
		}
		// Single-use: clear the cookie immediately so a leaked code+state
		// pair cannot be replayed.
		d.Cookies.SetOIDCState(c.Writer, "", -1)

		env, err := d.OIDCSigner.Verify(signed)
		if err != nil {
			writeErr(c, http.StatusBadRequest, "state_invalid", "")
			return
		}
		if env.State != state {
			writeErr(c, http.StatusBadRequest, "state_mismatch", "")
			return
		}
		claims, err := d.GoogleVerif.ExchangeAndVerify(c.Request.Context(), code, env.CodeVerifier, env.Nonce)
		if err != nil {
			writeErr(c, http.StatusBadRequest, "oidc_exchange_failed", err.Error())
			return
		}
		_, issued, err := d.Service.LoginViaGoogle(
			c.Request.Context(),
			claims,
			c.Request.UserAgent(), c.ClientIP(),
		)
		if err != nil {
			status, code := mapServiceError(err)
			writeErr(c, status, code, "")
			return
		}
		setCredentialCookies(c, d, issued)

		next := safeNextURL(env.Next)
		c.Redirect(http.StatusFound, next)
	}
}

// safeNextURL accepts only relative paths starting with "/" and not "//"
// (which would be a protocol-relative external redirect). Any other input
// falls back to "/" so the OIDC return cannot be turned into an open
// redirect.
func safeNextURL(raw string) string {
	if raw == "" {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	if _, err := url.Parse(raw); err != nil {
		return "/"
	}
	return raw
}
