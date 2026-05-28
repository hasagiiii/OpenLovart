package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the typed claim set we put on every access token. Standard
// fields (exp, iat, nbf, sub, jti) come from RegisteredClaims; the rest are
// project-specific.
type Claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	jwt.RegisteredClaims
}

// IssueAccess mints an RS256 access token with the active key. The returned
// JTI is also embedded in the token (so a future logout-by-jti flow can
// invalidate a single access token without changing this signature).
func IssueAccess(
	ks *KeyStore,
	userID uuid.UUID,
	email string,
	emailVerified bool,
	ttl time.Duration,
) (token string, jti uuid.UUID, exp time.Time, err error) {
	if ks == nil {
		return "", uuid.Nil, time.Time{}, errors.New("jwt: KeyStore must not be nil")
	}
	if ttl <= 0 {
		return "", uuid.Nil, time.Time{}, errors.New("jwt: ttl must be > 0")
	}

	now := time.Now().UTC()
	exp = now.Add(ttl)
	jti = uuid.New()

	claims := Claims{
		Email:         email,
		EmailVerified: emailVerified,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			ID:        jti.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}

	priv, kid := ks.SigningKey()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid

	signed, err := tok.SignedString(priv)
	if err != nil {
		return "", uuid.Nil, time.Time{}, fmt.Errorf("jwt: sign: %w", err)
	}
	return signed, jti, exp, nil
}

// Verify parses + validates a JWT, resolving the public key by the `kid`
// header. Tokens without a kid, with an unknown kid, or with a non-RS256
// alg are rejected with a non-nil error.
func Verify(ks *KeyStore, token string) (*Claims, error) {
	if ks == nil {
		return nil, errors.New("jwt: KeyStore must not be nil")
	}
	parsed, err := jwt.ParseWithClaims(
		token,
		&Claims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
			}
			kidRaw, ok := t.Header["kid"]
			if !ok {
				return nil, errors.New("missing kid header")
			}
			kid, ok := kidRaw.(string)
			if !ok {
				return nil, errors.New("kid header is not a string")
			}
			pub, ok := ks.FindPublicKey(kid)
			if !ok {
				return nil, fmt.Errorf("unknown kid %q", kid)
			}
			return pub, nil
		},
		jwt.WithValidMethods([]string{"RS256"}),
	)
	if err != nil {
		return nil, fmt.Errorf("jwt: verify: %w", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("jwt: invalid claims")
	}
	return claims, nil
}
