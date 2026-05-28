package oidc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// StateEnvelope is the payload we round-trip via the short-lived OIDC state
// cookie: it captures the random `state`, the PKCE verifier, the nonce, and
// the next-URL the user wanted before being bounced to Google.
//
// The struct is HMAC-signed (using cfg.AuthOIDCStateSecret — independent of
// JWT keys, see design.md D3) and base64url-encoded into the cookie value.
// Any tampering invalidates the HMAC; expiry is enforced by both the cookie
// Max-Age and the embedded NotAfter timestamp.
type StateEnvelope struct {
	State        string `json:"state"`
	CodeVerifier string `json:"cv"`
	Nonce        string `json:"n"`
	Next         string `json:"next,omitempty"`
	NotAfter     int64  `json:"exp"`
}

// StateTTL is how long the OIDC state cookie remains valid. 10 min is more
// than enough for any honest user to complete the Google round-trip while
// keeping the attacker's window narrow.
const StateTTL = 10 * time.Minute

// StateSigner produces and verifies signed state envelopes.
type StateSigner struct {
	hmacKey []byte
}

// NewStateSigner returns a StateSigner using the provided HMAC secret. The
// caller is expected to source the secret from cfg.AuthOIDCStateSecret;
// validation that it is ≥32 bytes happens at config-load time.
func NewStateSigner(secret string) *StateSigner {
	return &StateSigner{hmacKey: []byte(secret)}
}

// Sign serializes the envelope to JSON, signs the JSON with HMAC-SHA256,
// and returns `<base64url(json)>.<base64url(mac)>` ready to be stored in
// the state cookie.
func (s *StateSigner) Sign(env StateEnvelope) (string, error) {
	if env.NotAfter == 0 {
		env.NotAfter = time.Now().Add(StateTTL).Unix()
	}
	body, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("oidc: marshal state: %w", err)
	}
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write(body)
	tag := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(body) + "." +
		base64.RawURLEncoding.EncodeToString(tag), nil
}

// Verify decodes a signed state cookie value, checks the HMAC, and rejects
// expired envelopes. On success the original envelope is returned.
func (s *StateSigner) Verify(value string) (*StateEnvelope, error) {
	for i := 0; i < len(value); i++ {
		if value[i] == '.' {
			body64, tag64 := value[:i], value[i+1:]
			body, err := base64.RawURLEncoding.DecodeString(body64)
			if err != nil {
				return nil, fmt.Errorf("oidc: decode state body: %w", err)
			}
			tag, err := base64.RawURLEncoding.DecodeString(tag64)
			if err != nil {
				return nil, fmt.Errorf("oidc: decode state mac: %w", err)
			}
			mac := hmac.New(sha256.New, s.hmacKey)
			mac.Write(body)
			if !hmac.Equal(mac.Sum(nil), tag) {
				return nil, errors.New("oidc: state mac mismatch")
			}
			var env StateEnvelope
			if err := json.Unmarshal(body, &env); err != nil {
				return nil, fmt.Errorf("oidc: unmarshal state: %w", err)
			}
			if env.NotAfter > 0 && time.Now().Unix() > env.NotAfter {
				return nil, errors.New("oidc: state expired")
			}
			return &env, nil
		}
	}
	return nil, errors.New("oidc: malformed state cookie")
}

// ----- helpers used by the start handler -----

// stateBytes is the random size for state and nonce values (≈32 chars b64url).
const stateBytes = 24

// codeVerifierBytes is the PKCE code-verifier entropy (96-byte random string,
// >43 base64url chars and well under the 128 max).
const codeVerifierBytes = 64

// NewRandomString returns a base64url-encoded random string of n bytes of
// entropy. Used for both state and nonce.
func NewRandomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: read rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// NewState returns a fresh random state value.
func NewState() (string, error) { return NewRandomString(stateBytes) }

// NewNonce returns a fresh random nonce value.
func NewNonce() (string, error) { return NewRandomString(stateBytes) }

// NewCodeVerifier returns a fresh PKCE code verifier (random base64url string).
func NewCodeVerifier() (string, error) { return NewRandomString(codeVerifierBytes) }

// CodeChallengeS256 derives the S256 PKCE code challenge from a verifier.
func CodeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
