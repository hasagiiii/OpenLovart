// Package cookies centralizes auth-related cookie wiring. Every Set* helper
// reads the same Config so that __Host- prefix, Secure, SameSite, Path, and
// Domain stay consistent across handlers.
//
// Cookie inventory (matches design.md D1 & D5):
//
//	__Host-access  (HttpOnly)  — RS256 access token JWT.
//	__Host-refresh (HttpOnly, Path=/api/auth/refresh) — opaque refresh token.
//	csrf           (NOT HttpOnly) — double-submit CSRF token.
//	__Host-oidc-state (HttpOnly, short-lived) — OIDC PKCE/state envelope.
//
// Note: __Host- prefix requires Path=/, no Domain attribute, and Secure.
// In dev (cfg.AuthCookieSecure=false) we drop the prefix and the Secure
// attribute so cookies still work over plain http://localhost.
package cookies

import (
	"net/http"
	"time"

	"github.com/jiantaoli/openlovart/backend/internal/config"
)

// Cookie names. The "Host" variants are picked dynamically below based on
// cfg.AuthCookieSecure; the constants here are the wire-level names we emit.
const (
	cookieAccessHost     = "__Host-access"
	cookieAccessDev      = "access"
	cookieRefreshHost    = "__Host-refresh"
	cookieRefreshDev     = "refresh"
	cookieCSRFName       = "csrf"
	cookieOIDCStateHost  = "__Host-oidc-state"
	cookieOIDCStateDev   = "oidc-state"

	// RefreshPath narrows the refresh cookie to the rotation endpoint so it
	// is never sent on any other request.
	RefreshPath = "/api/auth/refresh"
)

// Manager owns the cookie configuration. Construct one at startup and pass
// it into handlers via dependency injection.
type Manager struct {
	cfg *config.Config
}

// New returns a Manager bound to cfg. Manager itself holds no state beyond
// the config pointer; it is safe for concurrent use.
func New(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// AccessName returns the wire-level cookie name in use for access tokens.
func (m *Manager) AccessName() string {
	if m.cfg.AuthCookieSecure {
		return cookieAccessHost
	}
	return cookieAccessDev
}

// RefreshName returns the wire-level cookie name in use for refresh tokens.
func (m *Manager) RefreshName() string {
	if m.cfg.AuthCookieSecure {
		return cookieRefreshHost
	}
	return cookieRefreshDev
}

// CSRFName returns the cookie name used for the double-submit CSRF token.
func (m *Manager) CSRFName() string { return cookieCSRFName }

// OIDCStateName returns the cookie name used for the short-lived OIDC state.
func (m *Manager) OIDCStateName() string {
	if m.cfg.AuthCookieSecure {
		return cookieOIDCStateHost
	}
	return cookieOIDCStateDev
}

// SetAccess writes the access-token cookie.
func (m *Manager) SetAccess(w http.ResponseWriter, value string) {
	http.SetCookie(w, m.build(m.AccessName(), value, "/", m.cfg.AuthAccessTTL, true))
}

// SetRefresh writes the refresh-token cookie. Path is narrowed so the cookie
// is only sent on /api/auth/refresh.
func (m *Manager) SetRefresh(w http.ResponseWriter, value string) {
	http.SetCookie(w, m.build(m.RefreshName(), value, RefreshPath, m.cfg.AuthRefreshTTL, true))
}

// SetCSRF writes the double-submit CSRF cookie. NOT HttpOnly — the frontend
// must read it from document.cookie to mirror it into the X-CSRF-Token header.
// Lifetime is tied to access-token TTL: a valid CSRF token implies the user
// could have a valid access token.
func (m *Manager) SetCSRF(w http.ResponseWriter, value string) {
	c := m.build(m.CSRFName(), value, "/", m.cfg.AuthAccessTTL, false)
	c.HttpOnly = false
	// __Host- prefix would forbid SetCSRF from being read by JS via
	// document.cookie (HttpOnly conflict avoided above), but we still keep
	// the cookie name plain `csrf` (no host prefix) since it must be readable.
	c.Name = cookieCSRFName
	http.SetCookie(w, c)
}

// SetOIDCState writes the very short-lived OIDC state envelope cookie.
func (m *Manager) SetOIDCState(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, m.build(m.OIDCStateName(), value, "/", ttl, true))
}

// ClearAll expires every auth cookie (used on logout).
func (m *Manager) ClearAll(w http.ResponseWriter) {
	expire := func(name, path string) {
		c := m.build(name, "", path, -time.Hour, true)
		c.MaxAge = -1
		http.SetCookie(w, c)
	}
	expire(m.AccessName(), "/")
	expire(m.RefreshName(), RefreshPath)
	expire(m.OIDCStateName(), "/")

	csrf := m.build(m.CSRFName(), "", "/", -time.Hour, false)
	csrf.HttpOnly = false
	csrf.MaxAge = -1
	http.SetCookie(w, csrf)
}

// build is the single source of truth for SameSite/Secure/Domain decisions.
// Callers should never construct an http.Cookie directly.
func (m *Manager) build(name, value, path string, ttl time.Duration, httpOnly bool) *http.Cookie {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: httpOnly,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.cfg.AuthCookieSecure,
	}
	// __Host- prefix forbids the Domain attribute, so we leave it unset
	// when AuthCookieSecure is true (which is what makes the prefix safe).
	if !m.cfg.AuthCookieSecure && m.cfg.AuthCookieDomain != "" {
		c.Domain = m.cfg.AuthCookieDomain
	}
	return c
}
