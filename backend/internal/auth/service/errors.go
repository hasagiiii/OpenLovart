// Package service holds the auth orchestration layer. It composes the lower
// primitives (password, jwt, refresh, oidc, email/templates) into the
// request-shaped methods that handlers call. Each service method returns a
// typed error from this file; handlers map those to coarse HTTP responses
// (400/401/403/409/422/429) without leaking internal detail.
package service

import "errors"

var (
	// ErrInvalidCredentials covers both "user not found" and "wrong password"
	// at the service layer. Handlers MUST NOT differentiate the two in
	// responses (avoid email enumeration).
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrEmailAlreadyRegistered is returned only to internal callers (and
	// tests). The /register handler intentionally swallows it and replies
	// with the same generic success envelope it would for a new user, then
	// emails the existing-account owner instead.
	ErrEmailAlreadyRegistered = errors.New("auth: email already registered")

	// ErrEmailNotVerified is returned by Login when the user exists but the
	// password identity exists alongside an unverified email address that
	// the policy requires verified.
	ErrEmailNotVerified = errors.New("auth: email not verified")

	// ErrTokenInvalid covers verify-email / reset-password lookups that did
	// not find a row, found an expired one, or found one already used.
	ErrTokenInvalid = errors.New("auth: token invalid or expired")

	// ErrPasswordWeak is returned when the proposed password fails the
	// service-level policy (length, blocklist).
	ErrPasswordWeak = errors.New("auth: password does not meet policy")

	// ErrOIDCEmailUnverified is the typed surface for "Google says
	// email_verified=false". The OIDC merge path requires a verified email
	// before linking to an existing local user.
	ErrOIDCEmailUnverified = errors.New("auth: oidc email not verified")
)
