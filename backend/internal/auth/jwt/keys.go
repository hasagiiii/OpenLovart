// Package jwt issues and verifies RS256 access tokens and exposes the
// matching public keys via JWKS. Signing keys live in a KeyStore that can
// either load a configured PEM keypair (prod) or auto-generate a 2048-bit
// pair under backend/.dev-keys/ on first run (dev convenience).
package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiantaoli/openlovart/backend/internal/config"
)

// PublicKey couples a verification key with its kid. Returned by
// KeyStore.PublicKeys() and consumed both by Verify (to look up the right
// key by kid header) and by jwks.go (to render the JWK Set).
type PublicKey struct {
	KID string
	Key *rsa.PublicKey
}

// KeyStore holds the active signing keypair and (eventually) any
// previous-generation public keys still trusted for verification. The
// current implementation tracks a single active key; the PublicKeys slice is
// already a slice so adding rotation in a future change requires no API
// break.
type KeyStore struct {
	activeKID  string
	privateKey *rsa.PrivateKey

	// publicKeys is ordered: the active key is first. Future rotation can
	// append the previously-active key here so freshly-issued tokens use the
	// new key while in-flight tokens continue to verify.
	publicKeys []PublicKey
}

// SigningKey returns the active private key + its kid. Callers must include
// kid in the JWT header so verifiers can resolve the matching public key.
func (k *KeyStore) SigningKey() (*rsa.PrivateKey, string) {
	return k.privateKey, k.activeKID
}

// PublicKeys returns the slice of trusted verification keys.
func (k *KeyStore) PublicKeys() []PublicKey {
	out := make([]PublicKey, len(k.publicKeys))
	copy(out, k.publicKeys)
	return out
}

// FindPublicKey resolves a kid to its trusted public key (or false).
func (k *KeyStore) FindPublicKey(kid string) (*rsa.PublicKey, bool) {
	for _, p := range k.publicKeys {
		if p.KID == kid {
			return p.Key, true
		}
	}
	return nil, false
}

// devKeyDir is the directory used to persist the auto-generated dev
// keypair. Resolved relative to the process working directory so a
// `cd backend && go run ./cmd/server` invocation finds the same keys
// across restarts.
const (
	devKeyDir       = ".dev-keys"
	devPrivKeyName  = "jwt.key"
	devPubKeyName   = "jwt.pub"
	devKeyDirMode   = 0o700
	devPrivKeyMode  = 0o600
	devPubKeyMode   = 0o644
	devKeyBitLength = 2048
)

// NewKeyStore constructs a KeyStore from configuration.
//
// Prod (cfg.Env != dev): both AUTH_JWT_PRIVATE_KEY_PATH and
// AUTH_JWT_PUBLIC_KEY_PATH must be set and readable PEM files; we load them.
// Dev with both paths empty: bootstrap a keypair into backend/.dev-keys/ on
// first run and reuse it on subsequent runs.
// Dev with both paths set: load them like prod (lets a developer pin a known
// keypair across resets).
func NewKeyStore(cfg *config.Config) (*KeyStore, error) {
	priv := strings.TrimSpace(cfg.AuthJWTPrivateKeyPath)
	pub := strings.TrimSpace(cfg.AuthJWTPublicKeyPath)

	if priv != "" && pub != "" {
		return loadFromPEM(priv, pub)
	}
	if cfg.IsDev() {
		return bootstrapDevKeys()
	}
	return nil, errors.New("jwt: AUTH_JWT_PRIVATE_KEY_PATH and AUTH_JWT_PUBLIC_KEY_PATH must be set in non-dev environments")
}

// loadFromPEM reads a PEM-encoded RSA keypair from disk.
func loadFromPEM(privPath, pubPath string) (*KeyStore, error) {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("jwt: read private key %q: %w", privPath, err)
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("jwt: read public key %q: %w", pubPath, err)
	}
	privKey, err := parseRSAPrivateKeyPEM(privPEM)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse private key: %w", err)
	}
	pubKey, err := parseRSAPublicKeyPEM(pubPEM)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse public key: %w", err)
	}
	if privKey.PublicKey.N.Cmp(pubKey.N) != 0 || privKey.PublicKey.E != pubKey.E {
		return nil, errors.New("jwt: private and public key files do not match")
	}
	kid, err := computeKID(pubKey)
	if err != nil {
		return nil, fmt.Errorf("jwt: compute kid: %w", err)
	}
	return &KeyStore{
		activeKID:  kid,
		privateKey: privKey,
		publicKeys: []PublicKey{{KID: kid, Key: pubKey}},
	}, nil
}

// bootstrapDevKeys returns the existing dev keypair if present, otherwise
// generates a fresh one. The generated keys are written to disk so all
// subsequent process restarts (and the JWKS endpoint, and the frontend's
// JWKS cache) see a stable kid.
func bootstrapDevKeys() (*KeyStore, error) {
	privPath := filepath.Join(devKeyDir, devPrivKeyName)
	pubPath := filepath.Join(devKeyDir, devPubKeyName)

	if _, err := os.Stat(privPath); err == nil {
		// Reuse: hand off to the same loader as prod.
		return loadFromPEM(privPath, pubPath)
	}

	if err := os.MkdirAll(devKeyDir, devKeyDirMode); err != nil {
		return nil, fmt.Errorf("jwt: mkdir %q: %w", devKeyDir, err)
	}
	priv, err := rsa.GenerateKey(rand.Reader, devKeyBitLength)
	if err != nil {
		return nil, fmt.Errorf("jwt: generate dev RSA key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("jwt: marshal dev private key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("jwt: marshal dev public key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	if err := os.WriteFile(privPath, privPEM, devPrivKeyMode); err != nil {
		return nil, fmt.Errorf("jwt: write dev private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, devPubKeyMode); err != nil {
		return nil, fmt.Errorf("jwt: write dev public key: %w", err)
	}

	kid, err := computeKID(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("jwt: compute kid: %w", err)
	}
	return &KeyStore{
		activeKID:  kid,
		privateKey: priv,
		publicKeys: []PublicKey{{KID: kid, Key: &priv.PublicKey}},
	}, nil
}

func parseRSAPrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("expected RSA private key, got %T", key)
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

func parseRSAPublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	switch block.Type {
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("expected RSA public key, got %T", key)
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// computeKID derives a stable, URL-safe kid from a public key. We hash the
// PKIX-DER encoding so the kid does not depend on PEM whitespace, then
// truncate to 22 base64url chars (≈132 random-equivalent bits — plenty for
// collision resistance).
func computeKID(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	enc := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(enc) < 22 {
		return enc, nil
	}
	return enc[:22], nil
}
