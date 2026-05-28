package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gjwt "github.com/golang-jwt/jwt/v5"

	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
)

// jwkSet mirrors the structure emitted by jwt.BuildJWKSet so we can decode
// the JWKS response without importing the package's private types.
type jwkSet struct {
	Keys []struct {
		Kty string `json:"kty"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		KID string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// ---------- 1) JWKS responds 200 unauthenticated -------------------------

func TestJWKSEndpointPublic200(t *testing.T) {
	s := newStack(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/.well-known/jwks.json", nil)
	rec := s.do(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("jwks: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("jwks: content-type=%q", ct)
	}
	var set jwkSet
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("decode jwks: %v", err)
	}
	if len(set.Keys) == 0 {
		t.Fatalf("jwks must list at least one key")
	}
	first := set.Keys[0]
	if first.Kty != "RSA" || first.Use != "sig" || first.Alg != "RS256" || first.KID == "" {
		t.Fatalf("first key has wrong shape: %+v", first)
	}
}

// ---------- 2) emitted kid matches a freshly issued access token's kid ---

func TestJWKSKIDMatchesFreshAccessTokenHeader(t *testing.T) {
	s := newStack(t)

	rec := s.do(httptest.NewRequest(http.MethodGet, "/api/auth/.well-known/jwks.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("jwks: %d", rec.Code)
	}
	var set jwkSet
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("decode jwks: %v", err)
	}
	if len(set.Keys) == 0 {
		t.Fatalf("jwks empty")
	}
	jwksKID := set.Keys[0].KID

	// Mint a new access token via the keystore exactly the way handlers do.
	tok, _, _, err := jwt.IssueAccess(s.ks, mustUUID(t), "k@example.com", true, time.Minute)
	if err != nil {
		t.Fatalf("issue access: %v", err)
	}

	// Decode the JWT header without verifying signature (we only care about
	// the kid claim).
	parser := gjwt.NewParser()
	parsed, _, err := parser.ParseUnverified(tok, gjwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}
	gotKID, ok := parsed.Header["kid"].(string)
	if !ok || gotKID == "" {
		t.Fatalf("access token missing kid header: %+v", parsed.Header)
	}
	if gotKID != jwksKID {
		t.Fatalf("jwks kid %q != access token kid %q", jwksKID, gotKID)
	}
}

// ---------- 3) Cache-Control: public, max-age=300 ------------------------

func TestJWKSCacheControlHeader(t *testing.T) {
	s := newStack(t)
	rec := s.do(httptest.NewRequest(http.MethodGet, "/api/auth/.well-known/jwks.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("jwks: %d", rec.Code)
	}
	cc := rec.Header().Get("Cache-Control")
	if cc != "public, max-age=300" {
		t.Fatalf("Cache-Control = %q, want %q", cc, "public, max-age=300")
	}
}
