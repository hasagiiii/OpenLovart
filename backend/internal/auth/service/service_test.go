package service_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	"github.com/jiantaoli/openlovart/backend/internal/auth/oidc"
	"github.com/jiantaoli/openlovart/backend/internal/auth/refresh"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/models"
	"github.com/jiantaoli/openlovart/backend/internal/testutil"
)

// fixture bundles the live service plus its collaborators so each test can
// reach into the pieces it needs (mailer for token capture, keystore for
// access-token decoding, db for direct row inspection).
type fixture struct {
	svc    *service.Service
	mailer *testutil.FakeMailer
	ks     *jwt.KeyStore
	db     *gorm.DB
}

// newFixture spins up a fresh container + service for one test. The cost
// is bounded by docker pull (cached after the first test) + Postgres boot
// (~2 s on warm cache).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	cfg := testutil.DevConfig()
	gdb := testutil.PostgresDB(t)
	ks := testutil.FreshKeyStore(t, cfg)
	mailer := testutil.NewFakeMailer()
	svc := service.New(gdb, cfg, ks, mailer, nil)
	return &fixture{svc: svc, mailer: mailer, ks: ks, db: gdb}
}

// ---------- 1) register-then-verify-then-login ----------------------------

// linkRE pulls a verify/reset URL out of the rendered HTML/text body.
var linkRE = regexp.MustCompile(`https?://[^\s"'<>]+`)

func TestRegisterVerifyAndLogin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	user, issued, err := f.svc.Register(ctx, "alice@example.com", "password-1234", "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("expected normalized email, got %q", user.Email)
	}
	if user.EmailVerifiedAt != nil {
		t.Fatalf("brand-new user should not be email-verified yet")
	}
	if issued.AccessToken == "" || issued.RefreshToken == "" || issued.CSRFToken == "" {
		t.Fatalf("expected full token trio, got %+v", issued)
	}

	// Capture the verification link from the queued email.
	mail, ok := f.mailer.Last()
	if !ok {
		t.Fatalf("expected verification email to be queued")
	}
	link := linkRE.FindString(mail.HTML + mail.Text)
	if link == "" {
		t.Fatalf("no verification link in email body: %s / %s", mail.HTML, mail.Text)
	}
	tok := extractTokenParam(t, link)

	if err := f.svc.VerifyEmail(ctx, tok); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// Re-load user; email_verified_at should now be set.
	var reloaded models.User
	if err := f.db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.EmailVerifiedAt == nil {
		t.Fatalf("expected email_verified_at to be populated after verify")
	}

	// Subsequent login should succeed and emit fresh credentials.
	_, login2, err := f.svc.Login(ctx, "alice@example.com", "password-1234", "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("login post-verify: %v", err)
	}
	if login2.AccessToken == issued.AccessToken {
		t.Fatalf("expected fresh access token after login")
	}
}

// ---------- 2) register-existing-email (typed error path) ----------------

func TestRegisterExistingEmailReturnsTypedError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, _, err := f.svc.Register(ctx, "dup@example.com", "password-1234", "", ""); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, _, err := f.svc.Register(ctx, "dup@example.com", "password-1234", "", "")
	if !errors.Is(err, service.ErrEmailAlreadyRegistered) {
		t.Fatalf("expected ErrEmailAlreadyRegistered, got %v", err)
	}
}

// ---------- 3) login-wrong-password --------------------------------------

func TestLoginWrongPassword(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := f.svc.Register(ctx, "bob@example.com", "password-1234", "", ""); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, _, err := f.svc.Login(ctx, "bob@example.com", "wrong-password", "", "")
	if !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	// Unknown email also surfaces the same error (anti-enumeration).
	_, _, err = f.svc.Login(ctx, "ghost@example.com", "anything", "", "")
	if !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for unknown email, got %v", err)
	}
}

// ---------- 4) login rate-limited (limiter contract) ---------------------

// The service itself does not gate the login per IP — that lives in the
// router middleware. We assert the limiter primitive (wired into the same
// route in production) does block past its burst, so test 14.2 can verify
// the HTTP-shape (Retry-After + 429) without re-implementing the math.
func TestLoginRateLimitedAtLimiter(t *testing.T) {
	limiter := newCappedLimiter(t)
	ok, _ := limiter.Allow(context.Background(), "ip-1.2.3.4")
	if !ok {
		t.Fatalf("first request should be allowed")
	}
	denied := false
	for i := 0; i < 5; i++ {
		ok, _ := limiter.Allow(context.Background(), "ip-1.2.3.4")
		if !ok {
			denied = true
			break
		}
	}
	if !denied {
		t.Fatalf("expected limiter to deny after burst")
	}
}

// ---------- 5) refresh success -------------------------------------------

func TestRefreshSuccessRotates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, first, err := f.svc.Register(ctx, "carol@example.com", "password-1234", "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_, second, err := f.svc.Refresh(ctx, first.RefreshToken, "", "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh did not rotate token")
	}
	if second.AccessToken == first.AccessToken {
		t.Fatalf("refresh did not mint fresh access token")
	}
}

// ---------- 6) refresh-reuse-detected-revokes-chain ----------------------

func TestRefreshReuseRevokesEntireChain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	user, first, err := f.svc.Register(ctx, "dan@example.com", "password-1234", "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Rotate once normally.
	_, second, err := f.svc.Refresh(ctx, first.RefreshToken, "", "")
	if err != nil {
		t.Fatalf("first rotate: %v", err)
	}
	// Replay the original token: should trip reuse detection AND revoke
	// every refresh token for the user.
	_, _, err = f.svc.Refresh(ctx, first.RefreshToken, "", "")
	if !errors.Is(err, refresh.ErrReuseDetected) {
		t.Fatalf("expected ErrReuseDetected on reuse, got %v", err)
	}
	// The successor (second) must now also be unusable.
	_, _, err = f.svc.Refresh(ctx, second.RefreshToken, "", "")
	if err == nil {
		t.Fatalf("expected reuse-chain revocation to invalidate successor")
	}
	// All refresh rows for the user should be revoked.
	var rows []models.RefreshToken
	if err := f.db.Where("user_id = ?", user.ID).Find(&rows).Error; err != nil {
		t.Fatalf("list refresh rows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected refresh rows for user")
	}
	for _, r := range rows {
		if r.RevokedAt == nil {
			t.Fatalf("refresh row %s not revoked after reuse detection", r.ID)
		}
	}
}

// ---------- 7) logout-revokes-refresh ------------------------------------

func TestLogoutRevokesRefresh(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, issued, err := f.svc.Register(ctx, "eve@example.com", "password-1234", "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := f.svc.Logout(ctx, issued.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	// Replaying it should fail (token is revoked).
	_, _, err = f.svc.Refresh(ctx, issued.RefreshToken, "", "")
	if err == nil {
		t.Fatalf("expected refresh after logout to fail")
	}
	// Idempotent: second logout with same token still returns nil.
	if err := f.svc.Logout(ctx, issued.RefreshToken); err != nil {
		t.Fatalf("second logout (idempotent): %v", err)
	}
}

// ---------- 8) OIDC merge into verified user -----------------------------

func TestOIDCMergeIntoExistingVerifiedUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Pre-create an email-verified user via password flow.
	user, _, err := f.svc.Register(ctx, "merge@example.com", "password-1234", "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	now := time.Now().UTC()
	if err := f.db.Model(&models.User{}).Where("id = ?", user.ID).Update("email_verified_at", now).Error; err != nil {
		t.Fatalf("force-verify: %v", err)
	}

	claims := &oidc.GoogleClaims{
		Sub:           "google-sub-merge",
		Email:         "merge@example.com",
		EmailVerified: true,
	}
	merged, _, err := f.svc.LoginViaGoogle(ctx, claims, "", "")
	if err != nil {
		t.Fatalf("login via google: %v", err)
	}
	if merged.ID != user.ID {
		t.Fatalf("expected merge into existing user; got new id %s vs %s", merged.ID, user.ID)
	}
	// Identity for google should now exist for that user.
	var ident models.Identity
	if err := f.db.Where("user_id = ? AND provider = ?", user.ID, service.ProviderGoogle).First(&ident).Error; err != nil {
		t.Fatalf("expected google identity attached: %v", err)
	}
	if ident.Subject != "google-sub-merge" {
		t.Fatalf("expected sub to be persisted, got %q", ident.Subject)
	}
}

// ---------- 9) OIDC creates new user when no match -----------------------

func TestOIDCCreatesNewUserWhenNoMatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	claims := &oidc.GoogleClaims{
		Sub:           "google-sub-new",
		Email:         "fresh@example.com",
		EmailVerified: true,
	}
	user, issued, err := f.svc.LoginViaGoogle(ctx, claims, "", "")
	if err != nil {
		t.Fatalf("login via google: %v", err)
	}
	if user.EmailVerifiedAt == nil {
		t.Fatalf("expected new google user to be email-verified at creation")
	}
	if issued.AccessToken == "" {
		t.Fatalf("expected access token")
	}
	// Credits row should default to 1000.
	var credits models.UserCredits
	if err := f.db.Where("user_id = ?", user.ID).First(&credits).Error; err != nil {
		t.Fatalf("expected credits row: %v", err)
	}
	if credits.Credits != 1000 {
		t.Fatalf("expected starting credits 1000, got %d", credits.Credits)
	}
	// Repeated login resolves to the same user via the existing google
	// identity (no second user created).
	user2, _, err := f.svc.LoginViaGoogle(ctx, claims, "", "")
	if err != nil {
		t.Fatalf("second google login: %v", err)
	}
	if user2.ID != user.ID {
		t.Fatalf("expected idempotent google login to land on same user")
	}
}

// ---------- 10) forgot-then-reset-revokes-all-refresh --------------------

func TestForgotThenResetRevokesAllRefresh(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	user, first, err := f.svc.Register(ctx, "frank@example.com", "password-1234", "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Force the welcome email out of the mailer queue so we can find the
	// reset link cleanly.
	f.mailer.Reset()

	if err := f.svc.RequestPasswordReset(ctx, "frank@example.com"); err != nil {
		t.Fatalf("forgot: %v", err)
	}
	mail, ok := f.mailer.Last()
	if !ok {
		t.Fatalf("expected reset email")
	}
	link := linkRE.FindString(mail.HTML + mail.Text)
	if link == "" {
		t.Fatalf("no link in reset email")
	}
	resetTok := extractTokenParam(t, link)

	if err := f.svc.ResetPassword(ctx, resetTok, "new-password-9876"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// Old refresh must now be revoked.
	_, _, err = f.svc.Refresh(ctx, first.RefreshToken, "", "")
	if err == nil {
		t.Fatalf("expected old refresh to be invalid post-reset")
	}
	// New password works; old does not.
	if _, _, err := f.svc.Login(ctx, "frank@example.com", "new-password-9876", "", ""); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if _, _, err := f.svc.Login(ctx, "frank@example.com", "password-1234", "", ""); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("old password should fail: %v", err)
	}
	// Anti-enumeration: forgot for unknown email is silent success.
	if err := f.svc.RequestPasswordReset(ctx, "no-such-user@example.com"); err != nil {
		t.Fatalf("forgot for unknown email should be silent success: %v", err)
	}
	_ = user
}
