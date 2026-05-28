package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
)

// JWK is the RFC 7517 JSON shape we emit for an RSA public key.
// Field names are the canonical JOSE names. Only the four members below are
// required for RS256 verification; we omit `kid` from the struct tag and set
// it explicitly so misordering can't silently drop it.
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	KID string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKSet is the JSON document at the JWKS endpoint.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// jwksCacheMaxAge is announced via Cache-Control on the JWKS response.
// 300 s matches the frontend's default JWKS cache TTL (AUTH_JWKS_CACHE_TTL),
// so a key rotation propagates within ~5 minutes without manual flushes.
const jwksCacheMaxAge = 300

// BuildJWKSet constructs the JSON-ready JWK Set from a KeyStore.
func BuildJWKSet(ks *KeyStore) JWKSet {
	pubs := ks.PublicKeys()
	out := JWKSet{Keys: make([]JWK, 0, len(pubs))}
	for _, p := range pubs {
		out.Keys = append(out.Keys, JWK{
			Kty: "RSA",
			Use: "sig",
			Alg: "RS256",
			KID: p.KID,
			N:   base64.RawURLEncoding.EncodeToString(p.Key.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.Key.E)).Bytes()),
		})
	}
	return out
}

// Handler returns an http.HandlerFunc that serves the JWKS document. The
// handler is intended to be mounted unauthenticated and ahead of any CSRF
// middleware (see internal/httpserver/router.go).
func Handler(ks *KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		set := BuildJWKSet(ks)
		body, err := json.Marshal(set)
		if err != nil {
			http.Error(w, "jwks marshal failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", jwksCacheMaxAge))
		_, _ = w.Write(body)
	}
}
