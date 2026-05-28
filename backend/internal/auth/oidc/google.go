// Package oidc implements the Google OpenID Connect login flow.
//
// Flow (per design.md D2):
//  1. Frontend hits /api/auth/oidc/google/start. We mint state+verifier+nonce,
//     stash them in a short-lived signed cookie (state.go), and 302 to
//     accounts.google.com with PKCE.
//  2. Google redirects back to /api/auth/oidc/google/callback?code=&state=.
//  3. Handler reads the cookie, verifies state matches, exchanges the code
//     using the verifier, then verifies the resulting ID token's nonce.
//  4. We require Google's `email_verified=true` before merging or creating
//     a local User row (see auth/service Phase 6 for the merge rules).
package oidc

import (
	"context"
	"errors"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	googleEndpoint "golang.org/x/oauth2/google"

	"github.com/jiantaoli/openlovart/backend/internal/config"
)

// GoogleClaims is the trimmed-down view we keep after a successful exchange.
type GoogleClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// GoogleVerifier wires together the OIDC provider, OAuth2 config, and
// ID-token verifier. Construct one at startup with NewGoogleVerifier and
// reuse it across requests.
type GoogleVerifier struct {
	oauthCfg    *oauth2.Config
	idVerifier  *gooidc.IDTokenVerifier
}

// NewGoogleVerifier discovers the Google OIDC provider, builds an
// oauth2.Config, and wraps a verifier with our ClientID. Returns an error
// if discovery fails (network/DNS) or required config is missing.
func NewGoogleVerifier(ctx context.Context, cfg *config.Config) (*GoogleVerifier, error) {
	if cfg.OIDCGoogleClientID == "" || cfg.OIDCGoogleClientSecret == "" || cfg.OIDCGoogleRedirectURL == "" {
		return nil, errors.New("oidc: OIDC_GOOGLE_CLIENT_ID/SECRET/REDIRECT_URL must all be set")
	}
	provider, err := gooidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("oidc: discover google provider: %w", err)
	}
	return &GoogleVerifier{
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.OIDCGoogleClientID,
			ClientSecret: cfg.OIDCGoogleClientSecret,
			RedirectURL:  cfg.OIDCGoogleRedirectURL,
			Endpoint:     googleEndpoint.Endpoint,
			Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
		},
		idVerifier: provider.Verifier(&gooidc.Config{ClientID: cfg.OIDCGoogleClientID}),
	}, nil
}

// BuildAuthURL constructs the URL we 302 the browser to. The caller must
// supply a state value (mirrored back by Google) and a PKCE code challenge
// (S256). Nonce is included in the auth request and re-checked on the ID
// token after exchange.
func (g *GoogleVerifier) BuildAuthURL(state, codeChallenge, nonce string) string {
	return g.oauthCfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		gooidc.Nonce(nonce),
	)
}

// ExchangeAndVerify runs the authorization-code → ID-token exchange and
// returns the verified Google claims. expectedNonce is compared against
// the `nonce` claim on the ID token.
func (g *GoogleVerifier) ExchangeAndVerify(
	ctx context.Context,
	code, codeVerifier, expectedNonce string,
) (*GoogleClaims, error) {
	tok, err := g.oauthCfg.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("oidc: exchange code: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, errors.New("oidc: missing id_token in token response")
	}
	idTok, err := g.idVerifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("oidc: verify id_token: %w", err)
	}
	if idTok.Nonce != expectedNonce {
		return nil, errors.New("oidc: nonce mismatch")
	}

	var claims GoogleClaims
	if err := idTok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: decode claims: %w", err)
	}
	if !claims.EmailVerified {
		return nil, errors.New("oidc: email_verified is false")
	}
	if claims.Sub == "" || claims.Email == "" {
		return nil, errors.New("oidc: claims missing sub or email")
	}
	return &claims, nil
}
