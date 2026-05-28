package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/oidc"
)

// OIDCGoogleStart begins the Google OIDC handshake: mints state + PKCE
// verifier + nonce, stores them in the short-lived signed state cookie,
// and 302s to Google.
func OIDCGoogleStart(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		state, err := oidc.NewState()
		if err != nil {
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		nonce, err := oidc.NewNonce()
		if err != nil {
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		verifier, err := oidc.NewCodeVerifier()
		if err != nil {
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		challenge := oidc.CodeChallengeS256(verifier)

		env := oidc.StateEnvelope{
			State:        state,
			CodeVerifier: verifier,
			Nonce:        nonce,
			Next:         c.Query("next"),
		}
		signed, err := d.OIDCSigner.Sign(env)
		if err != nil {
			writeErr(c, http.StatusInternalServerError, "internal", "")
			return
		}
		d.Cookies.SetOIDCState(c.Writer, signed, oidc.StateTTL)

		url := d.GoogleVerif.BuildAuthURL(state, challenge, nonce)
		c.Redirect(http.StatusFound, url)
	}
}
