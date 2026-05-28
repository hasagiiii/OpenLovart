// Package csrf implements double-submit token issuance and verification.
// The token is a random 32-byte value, base64url-encoded. The same value
// lives in the `csrf` cookie (readable by JS) and is mirrored back by the
// browser in the `X-CSRF-Token` header on unsafe requests.
package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

const tokenBytes = 32

// IssueToken returns a freshly-generated, base64url-encoded random token.
func IssueToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("csrf: read rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Verify performs a constant-time comparison of the header- and cookie-side
// values. Empty inputs (either side) are always treated as a mismatch.
func Verify(headerValue, cookieValue string) bool {
	if headerValue == "" || cookieValue == "" {
		return false
	}
	if len(headerValue) != len(cookieValue) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(headerValue), []byte(cookieValue)) == 1
}
