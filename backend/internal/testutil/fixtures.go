package testutil

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	"github.com/jiantaoli/openlovart/backend/internal/config"
	"github.com/jiantaoli/openlovart/backend/internal/email"
)

// DevConfig returns a Config populated with values appropriate for unit
// tests: in-process URLs, generous TTLs, AUTH_COOKIE_SECURE=false so the
// dev cookie names ("access" / "refresh" instead of __Host- variants) are
// used, and a synthetic OIDC state secret.
func DevConfig() *config.Config {
	return &config.Config{
		Env:                  "dev",
		HTTPAddr:             ":0",
		LogLevel:             "info",
		DatabaseURL:          "postgres://placeholder",
		AuthAccessTTL:        15 * time.Minute,
		AuthRefreshTTL:       720 * time.Hour,
		AuthCookieSecure:     false,
		AuthOIDCStateSecret:  "test-state-secret-32-bytes-min!!!!",
		EmailVerifyBaseURL:   "http://localhost:3000/sign-in/verify-email",
		PasswordResetBaseURL: "http://localhost:3000/sign-in/reset-password",
		SMTPHost:             "localhost",
		SMTPPort:             1025,

		RateLimitLoginPerMin:     1000, // effectively disabled in tests
		RateLimitRegisterPerHour: 10000,
	}
}

// FreshKeyStore loads a KeyStore backed by a RAM-only RSA-2048 keypair.
// We persist the keys under a per-test temp dir and point the config's
// JWT path entries at them so KeyStore loadFromPEM is exercised end-to-end.
func FreshKeyStore(t *testing.T, cfg *config.Config) *jwt.KeyStore {
	t.Helper()
	dir := t.TempDir()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa: %v", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal priv: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	privPath := filepath.Join(dir, "jwt.key")
	pubPath := filepath.Join(dir, "jwt.pub")
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		t.Fatalf("write priv: %v", err)
	}
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644); err != nil {
		t.Fatalf("write pub: %v", err)
	}
	cfg.AuthJWTPrivateKeyPath = privPath
	cfg.AuthJWTPublicKeyPath = pubPath

	ks, err := jwt.NewKeyStore(cfg)
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	return ks
}

// CookieMgr is a tiny convenience to construct the cookie manager.
func CookieMgr(cfg *config.Config) *cookies.Manager { return cookies.New(cfg) }

// FakeMailer is a thread-safe in-memory email.Sender. Tests inspect Sent
// to assert template invocation and to fish verification / reset links out
// of the rendered HTML.
type FakeMailer struct {
	mu   sync.Mutex
	Sent []SentMail
}

// SentMail captures a single Send call.
type SentMail struct {
	To      string
	Subject string
	HTML    string
	Text    string
	At      time.Time
}

// NewFakeMailer constructs a FakeMailer.
func NewFakeMailer() *FakeMailer { return &FakeMailer{} }

// Send records the call and returns nil.
func (m *FakeMailer) Send(_ context.Context, to, subject, html, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, SentMail{To: to, Subject: subject, HTML: html, Text: text, At: time.Now()})
	return nil
}

// Last returns the most recent SentMail or false.
func (m *FakeMailer) Last() (SentMail, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Sent) == 0 {
		return SentMail{}, false
	}
	return m.Sent[len(m.Sent)-1], true
}

// Reset clears the captured Sent slice.
func (m *FakeMailer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = nil
}

var _ email.Sender = (*FakeMailer)(nil)
