// Package handlers turns the auth.Service methods into Gin HTTP handlers.
// Every handler returns the uniform error envelope `{error, message}` and
// pulls cookie/CSRF helpers from auth/cookies and auth/csrf so wire-level
// behavior stays consistent across endpoints.
package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
)

// errorBody is the canonical error envelope returned by every auth handler.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// writeErr writes status + the canonical error envelope.
func writeErr(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, errorBody{Error: code, Message: msg})
}

// mapServiceError translates service-layer errors to (status, code) pairs
// suitable for handler responses. Anything not mapped is treated as a 500
// to avoid leaking internal detail.
func mapServiceError(err error) (status int, code string) {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		return http.StatusUnauthorized, "invalid_credentials"
	case errors.Is(err, service.ErrEmailAlreadyRegistered):
		// Surfaces only in tests; production handlers swallow it.
		return http.StatusConflict, "email_taken"
	case errors.Is(err, service.ErrEmailNotVerified):
		return http.StatusForbidden, "email_not_verified"
	case errors.Is(err, service.ErrTokenInvalid):
		return http.StatusBadRequest, "token_invalid"
	case errors.Is(err, service.ErrPasswordWeak):
		return http.StatusUnprocessableEntity, "password_weak"
	case errors.Is(err, service.ErrOIDCEmailUnverified):
		return http.StatusForbidden, "oidc_email_unverified"
	default:
		return http.StatusInternalServerError, "internal"
	}
}
