package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	"github.com/jiantaoli/openlovart/backend/internal/auth/oidc"
	"github.com/jiantaoli/openlovart/backend/internal/auth/password"
	"github.com/jiantaoli/openlovart/backend/internal/auth/refresh"
	"github.com/jiantaoli/openlovart/backend/internal/config"
	"github.com/jiantaoli/openlovart/backend/internal/email"
	"github.com/jiantaoli/openlovart/backend/internal/email/templates"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// Provider names stored in identities.provider.
const (
	ProviderPassword = "password"
	ProviderGoogle   = "google"
)

// Verification token lifetime. Mirrored in the email copy ("expires in N").
const (
	verifyTokenTTL = 24 * time.Hour
	resetTokenTTL  = 30 * time.Minute

	// startingCredits is the soft balance we grant every new user (matches
	// the Supabase trigger we are replacing).
	startingCredits = 1000
)

// Service is the auth orchestration façade.
type Service struct {
	db       *gorm.DB
	cfg      *config.Config
	keyStore *jwt.KeyStore
	mailer   email.Sender
	gv       *oidc.GoogleVerifier
}

// New wires a Service from its dependencies.
func New(
	db *gorm.DB,
	cfg *config.Config,
	keyStore *jwt.KeyStore,
	mailer email.Sender,
	googleVerifier *oidc.GoogleVerifier,
) *Service {
	return &Service{
		db:       db,
		cfg:      cfg,
		keyStore: keyStore,
		mailer:   mailer,
		gv:       googleVerifier,
	}
}

// IssuedTokens bundles the freshly minted credential trio returned by
// Login / Register / Refresh / LoginViaGoogle.
type IssuedTokens struct {
	AccessToken    string
	AccessExpires  time.Time
	RefreshToken   string
	CSRFToken      string
}

// ----- Register --------------------------------------------------------

// Register creates a new user + password identity + user_credits row in a
// single transaction, then issues credentials and queues a verification
// email. If the email is already registered the function returns
// ErrEmailAlreadyRegistered without modifying the database; the handler is
// responsible for translating that into a generic "check your inbox"
// response.
func (s *Service) Register(
	ctx context.Context,
	email, plainPassword, userAgent, ipAtIssue string,
) (*models.User, *IssuedTokens, error) {
	email = normalizeEmail(email)
	if err := validatePassword(plainPassword); err != nil {
		return nil, nil, err
	}

	hashed, err := password.Hash(plainPassword)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: hash password: %w", err)
	}

	var user models.User
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Fast existence check on email (citext makes this case-insensitive).
		var count int64
		if err := tx.Model(&models.User{}).Where("email = ?", email).Count(&count).Error; err != nil {
			return fmt.Errorf("count user by email: %w", err)
		}
		if count > 0 {
			return ErrEmailAlreadyRegistered
		}
		user = models.User{Email: email}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		if err := tx.Create(&models.Identity{
			UserID:   user.ID,
			Provider: ProviderPassword,
			Subject:  email,
			Secret:   strPtr(hashed),
		}).Error; err != nil {
			return fmt.Errorf("create password identity: %w", err)
		}
		if err := tx.Create(&models.UserCredits{
			UserID:  user.ID,
			Credits: startingCredits,
		}).Error; err != nil {
			return fmt.Errorf("create user_credits: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Best-effort verification email. We log+swallow inside the caller; the
	// service surfaces only a service-level error if mailer truly fails.
	if mailErr := s.sendVerificationEmail(ctx, &user); mailErr != nil {
		return nil, nil, fmt.Errorf("queue verification email: %w", mailErr)
	}

	issued, err := s.issueTokens(ctx, &user, userAgent, ipAtIssue)
	if err != nil {
		return nil, nil, err
	}
	return &user, issued, nil
}

// ----- Login -----------------------------------------------------------

// Login validates email + password and issues a fresh credential trio.
// Always returns ErrInvalidCredentials for unknown email or wrong password.
func (s *Service) Login(
	ctx context.Context,
	email, plainPassword, userAgent, ipAtIssue string,
) (*models.User, *IssuedTokens, error) {
	email = normalizeEmail(email)

	var user models.User
	if err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, fmt.Errorf("auth: lookup user: %w", err)
	}

	var ident models.Identity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND provider = ?", user.ID, ProviderPassword).
		First(&ident).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Email exists, but only via OIDC. Tell the caller invalid creds
			// (don't disclose that the account exists via Google).
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, fmt.Errorf("auth: lookup identity: %w", err)
	}
	if ident.Secret == nil {
		return nil, nil, ErrInvalidCredentials
	}

	ok, needsRehash, err := password.Verify(plainPassword, *ident.Secret)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: verify password: %w", err)
	}
	if !ok {
		return nil, nil, ErrInvalidCredentials
	}

	if needsRehash {
		// Best-effort rehash. Failure does not block login — log via the
		// caller's slog wiring.
		if newHash, err := password.Hash(plainPassword); err == nil {
			_ = s.db.WithContext(ctx).
				Model(&models.Identity{}).
				Where("id = ?", ident.ID).
				Update("secret", newHash).Error
		}
	}

	issued, err := s.issueTokens(ctx, &user, userAgent, ipAtIssue)
	if err != nil {
		return nil, nil, err
	}
	return &user, issued, nil
}

// ----- Refresh ---------------------------------------------------------

// Refresh rotates the presented refresh token and mints a new access JWT
// and CSRF token. Reuse propagates as refresh.ErrReuseDetected.
func (s *Service) Refresh(
	ctx context.Context,
	plainRefresh, userAgent, ipAtIssue string,
) (*models.User, *IssuedTokens, error) {
	newRefresh, row, err := refresh.Rotate(ctx, s.db, plainRefresh, s.cfg.AuthRefreshTTL, userAgent, ipAtIssue)
	if err != nil {
		return nil, nil, err
	}
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", row.UserID).Error; err != nil {
		return nil, nil, fmt.Errorf("auth: load user after refresh: %w", err)
	}
	access, _, exp, err := jwt.IssueAccess(s.keyStore, user.ID, user.Email, user.EmailVerifiedAt != nil, s.cfg.AuthAccessTTL)
	if err != nil {
		return nil, nil, err
	}
	csrfTok, err := freshCSRF()
	if err != nil {
		return nil, nil, err
	}
	return &user, &IssuedTokens{
		AccessToken:   access,
		AccessExpires: exp,
		RefreshToken:  newRefresh,
		CSRFToken:     csrfTok,
	}, nil
}

// ----- Logout ----------------------------------------------------------

// Logout revokes the presented refresh token. Idempotent: revoking an
// already-revoked or unknown token is treated as success.
func (s *Service) Logout(ctx context.Context, plainRefresh string) error {
	if plainRefresh == "" {
		return nil
	}
	return refresh.Revoke(ctx, s.db, plainRefresh)
}

// ----- Email verification ---------------------------------------------

// RequestEmailVerification regenerates a one-time token, persists its hash,
// and emails the user. Safe to call repeatedly; previous unused tokens
// remain valid until they expire (the user simply ends up with several
// click-able links — clicking any one verifies them).
func (s *Service) RequestEmailVerification(ctx context.Context, userID uuid.UUID) error {
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	return s.sendVerificationEmail(ctx, &user)
}

// VerifyEmail consumes a token: validates+marks-used in one transaction,
// then sets users.email_verified_at. Returns ErrTokenInvalid on
// not-found / expired / already-used.
func (s *Service) VerifyEmail(ctx context.Context, plainToken string) error {
	hash := sha256Bytes(plainToken)
	now := time.Now().UTC()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.VerificationToken
		err := tx.Where("token_hash = ?", hash).First(&row).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTokenInvalid
			}
			return fmt.Errorf("lookup verification token: %w", err)
		}
		if row.UsedAt != nil || !row.ExpiresAt.After(now) {
			return ErrTokenInvalid
		}
		if err := tx.Model(&row).Update("used_at", now).Error; err != nil {
			return fmt.Errorf("mark verification used: %w", err)
		}
		if err := tx.Model(&models.User{}).
			Where("id = ?", row.UserID).
			Update("email_verified_at", now).Error; err != nil {
			return fmt.Errorf("set email_verified_at: %w", err)
		}
		return nil
	})
}

// ----- Password reset --------------------------------------------------

// RequestPasswordReset always returns nil from the handler's perspective:
// the function is silent on whether the email exists (anti-enumeration).
// When the email matches a known user we persist a reset token + send mail.
func (s *Service) RequestPasswordReset(ctx context.Context, emailAddr string) error {
	emailAddr = normalizeEmail(emailAddr)

	var user models.User
	if err := s.db.WithContext(ctx).Where("email = ?", emailAddr).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // silent success — anti-enumeration
		}
		return fmt.Errorf("auth: lookup user for reset: %w", err)
	}

	plain, hash, err := newRandomToken()
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&models.PasswordResetToken{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().UTC().Add(resetTokenTTL),
	}).Error; err != nil {
		return fmt.Errorf("auth: persist reset token: %w", err)
	}

	link := fmt.Sprintf("%s?token=%s", s.cfg.PasswordResetBaseURL, plain)
	subject, html, text, err := templates.RenderReset(templates.ResetData{Link: link})
	if err != nil {
		return fmt.Errorf("auth: render reset email: %w", err)
	}
	return s.mailer.Send(ctx, user.Email, subject, html, text)
}

// ResetPassword consumes the reset token, sets a new password, and revokes
// every active refresh token for the user.
func (s *Service) ResetPassword(ctx context.Context, plainToken, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hashed, err := password.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}
	hash := sha256Bytes(plainToken)
	now := time.Now().UTC()

	var userID uuid.UUID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.PasswordResetToken
		err := tx.Where("token_hash = ?", hash).First(&row).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTokenInvalid
			}
			return fmt.Errorf("lookup reset token: %w", err)
		}
		if row.UsedAt != nil || !row.ExpiresAt.After(now) {
			return ErrTokenInvalid
		}
		userID = row.UserID

		if err := tx.Model(&row).Update("used_at", now).Error; err != nil {
			return fmt.Errorf("mark reset used: %w", err)
		}
		// Update existing password identity, or create one if the user only
		// had OIDC identities so far (lets a Google-only user attach a local
		// password by going through the reset flow with their verified email).
		res := tx.Model(&models.Identity{}).
			Where("user_id = ? AND provider = ?", userID, ProviderPassword).
			Update("secret", hashed)
		if res.Error != nil {
			return fmt.Errorf("update password identity: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			var user models.User
			if err := tx.First(&user, "id = ?", userID).Error; err != nil {
				return fmt.Errorf("load user for new identity: %w", err)
			}
			if err := tx.Create(&models.Identity{
				UserID:   userID,
				Provider: ProviderPassword,
				Subject:  user.Email,
				Secret:   strPtr(hashed),
			}).Error; err != nil {
				return fmt.Errorf("create password identity during reset: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Revoke every refresh outside the tx so its visibility is bounded by a
	// single short-lived UPDATE.
	return refresh.RevokeAllForUser(ctx, s.db, userID)
}

// ----- OIDC merge ------------------------------------------------------

// LoginViaGoogle is invoked by the OIDC callback handler after the ID token
// has been verified. Implements the design.md D8 merge rules:
//
//   - If a Google identity with this `sub` already exists, log that user in.
//   - Else, if a user with the same (verified) email exists, attach a new
//     google identity to it.
//   - Else, create a fresh user (+credits row) with a google identity.
//
// Google must already have asserted email_verified=true; the caller
// (oidc.GoogleVerifier) enforces that, so we trust claims.EmailVerified
// here. As a defense-in-depth check we still gate on it.
func (s *Service) LoginViaGoogle(
	ctx context.Context,
	claims *oidc.GoogleClaims,
	userAgent, ipAtIssue string,
) (*models.User, *IssuedTokens, error) {
	if claims == nil {
		return nil, nil, errors.New("auth: nil google claims")
	}
	if !claims.EmailVerified {
		return nil, nil, ErrOIDCEmailUnverified
	}
	emailAddr := normalizeEmail(claims.Email)

	var user models.User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1) Existing google identity?
		var ident models.Identity
		err := tx.Where("provider = ? AND subject = ?", ProviderGoogle, claims.Sub).First(&ident).Error
		switch {
		case err == nil:
			return tx.First(&user, "id = ?", ident.UserID).Error
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("lookup google identity: %w", err)
		}

		// 2) Existing user by email → attach.
		err = tx.Where("email = ?", emailAddr).First(&user).Error
		switch {
		case err == nil:
			now := time.Now().UTC()
			if user.EmailVerifiedAt == nil {
				if err := tx.Model(&user).Update("email_verified_at", now).Error; err != nil {
					return fmt.Errorf("verify email on merge: %w", err)
				}
				user.EmailVerifiedAt = &now
			}
			if err := tx.Create(&models.Identity{
				UserID:   user.ID,
				Provider: ProviderGoogle,
				Subject:  claims.Sub,
			}).Error; err != nil {
				return fmt.Errorf("attach google identity: %w", err)
			}
			return nil
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("lookup user by email: %w", err)
		}

		// 3) Brand-new user.
		now := time.Now().UTC()
		user = models.User{
			Email:           emailAddr,
			EmailVerifiedAt: &now,
		}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("create user for google: %w", err)
		}
		if err := tx.Create(&models.Identity{
			UserID:   user.ID,
			Provider: ProviderGoogle,
			Subject:  claims.Sub,
		}).Error; err != nil {
			return fmt.Errorf("create google identity: %w", err)
		}
		if err := tx.Create(&models.UserCredits{
			UserID:  user.ID,
			Credits: startingCredits,
		}).Error; err != nil {
			return fmt.Errorf("create user_credits for google user: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	issued, err := s.issueTokens(ctx, &user, userAgent, ipAtIssue)
	if err != nil {
		return nil, nil, err
	}
	return &user, issued, nil
}

// ----- Me --------------------------------------------------------------

// Me returns the authenticated user record by ID.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// ----- Internals -------------------------------------------------------

// issueTokens generates the access JWT, refresh row, and CSRF token in one
// place so Register/Login/LoginViaGoogle remain in lock-step.
func (s *Service) issueTokens(
	ctx context.Context,
	user *models.User,
	userAgent, ipAtIssue string,
) (*IssuedTokens, error) {
	access, _, exp, err := jwt.IssueAccess(
		s.keyStore,
		user.ID,
		user.Email,
		user.EmailVerifiedAt != nil,
		s.cfg.AuthAccessTTL,
	)
	if err != nil {
		return nil, err
	}
	refreshPlain, _, err := refresh.Issue(ctx, s.db, user.ID, s.cfg.AuthRefreshTTL, userAgent, ipAtIssue)
	if err != nil {
		return nil, err
	}
	csrfTok, err := freshCSRF()
	if err != nil {
		return nil, err
	}
	return &IssuedTokens{
		AccessToken:   access,
		AccessExpires: exp,
		RefreshToken:  refreshPlain,
		CSRFToken:     csrfTok,
	}, nil
}

func (s *Service) sendVerificationEmail(ctx context.Context, user *models.User) error {
	plain, hash, err := newRandomToken()
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&models.VerificationToken{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().UTC().Add(verifyTokenTTL),
	}).Error; err != nil {
		return fmt.Errorf("persist verify token: %w", err)
	}
	link := fmt.Sprintf("%s?token=%s", s.cfg.EmailVerifyBaseURL, plain)
	subject, html, text, err := templates.RenderVerify(templates.VerifyData{Link: link})
	if err != nil {
		return fmt.Errorf("render verify email: %w", err)
	}
	return s.mailer.Send(ctx, user.Email, subject, html, text)
}

// ----- helpers ---------------------------------------------------------

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func strPtr(s string) *string { return &s }

// validatePassword enforces the service-level policy: 8–128 chars, must
// include any non-whitespace content. Strength meters live on the frontend;
// this is the floor below which we reject outright.
func validatePassword(p string) error {
	if len(p) < 8 || len(p) > 128 {
		return ErrPasswordWeak
	}
	if strings.TrimSpace(p) == "" {
		return ErrPasswordWeak
	}
	return nil
}

// newRandomToken returns a random base64url plaintext + sha-256 hash, ready
// to be persisted as the (plaintext-emailed, hash-stored) pair used by both
// verification and password-reset flows.
func newRandomToken() (plain string, hash []byte, err error) {
	const tokenBytes = 32
	buf := make([]byte, tokenBytes)
	if _, err := readRand(buf); err != nil {
		return "", nil, err
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(plain))
	return plain, h[:], nil
}

// freshCSRF mints a new CSRF token. Implemented locally (rather than via the
// csrf package) so service has no router-side dependencies beyond crypto.
func freshCSRF() (string, error) {
	buf := make([]byte, 32)
	if _, err := readRand(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func sha256Bytes(plain string) []byte {
	h := sha256.Sum256([]byte(plain))
	return h[:]
}
