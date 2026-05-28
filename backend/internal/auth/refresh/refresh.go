// Package refresh issues, rotates, and revokes refresh tokens. The plaintext
// token is delivered exactly once (over the __Host-refresh cookie); the
// database stores only the sha-256 hash. Rotation is mandatory: every
// /api/auth/refresh succeeds by issuing a fresh token AND revoking the
// presented one, with a chain pointer so we can detect reuse and revoke the
// entire chain.
package refresh

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// ErrReuseDetected is returned by Rotate when the presented token has
// already been revoked. Callers should treat it as a security event:
// revoke every refresh row in the same chain (Rotate already does this) and
// force the user to re-authenticate.
var (
	ErrReuseDetected = errors.New("refresh: token reuse detected")
	ErrUnknownToken  = errors.New("refresh: token not found")
	ErrExpired       = errors.New("refresh: token expired")
)

// tokenBytes is the entropy of the plaintext refresh token (32 bytes ≈ 256
// bits). The plaintext is base64url-encoded; the database stores sha-256(plain).
const tokenBytes = 32

// Issue creates a fresh refresh row and returns the plaintext token (to be
// sent to the client) plus the persisted *RefreshToken row.
func Issue(
	ctx context.Context,
	db *gorm.DB,
	userID uuid.UUID,
	ttl time.Duration,
	userAgent, ipAtIssue string,
) (plain string, row *models.RefreshToken, err error) {
	if ttl <= 0 {
		return "", nil, errors.New("refresh: ttl must be > 0")
	}
	plain, hash, err := newTokenAndHash()
	if err != nil {
		return "", nil, err
	}
	row = &models.RefreshToken{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: time.Now().UTC().Add(ttl),
		UserAgent: userAgent,
		IPAtIssue: ipAtIssue,
	}
	if err := db.WithContext(ctx).Create(row).Error; err != nil {
		return "", nil, fmt.Errorf("refresh: insert: %w", err)
	}
	return plain, row, nil
}

// Rotate validates the presented plaintext token, marks it revoked, and
// issues a successor row that points back at it via ReplacedByID.
//
// Reuse semantics: if the presented token is already revoked, the entire
// chain (every row in the same lineage as the presented token) is revoked
// in one transaction and ErrReuseDetected is returned.
func Rotate(
	ctx context.Context,
	db *gorm.DB,
	plain string,
	ttl time.Duration,
	userAgent, ipAtIssue string,
) (newPlain string, newRow *models.RefreshToken, err error) {
	if ttl <= 0 {
		return "", nil, errors.New("refresh: ttl must be > 0")
	}
	hash := hashToken(plain)

	var presented models.RefreshToken
	if err := db.WithContext(ctx).
		Where("token_hash = ?", hash).
		First(&presented).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, ErrUnknownToken
		}
		return "", nil, fmt.Errorf("refresh: lookup: %w", err)
	}

	now := time.Now().UTC()
	if presented.RevokedAt != nil {
		// Reuse: revoke the entire chain by walking forward to the head.
		if err := revokeChainForUser(ctx, db, presented.UserID, now); err != nil {
			return "", nil, fmt.Errorf("refresh: revoke chain after reuse: %w", err)
		}
		return "", nil, ErrReuseDetected
	}
	if !presented.ExpiresAt.After(now) {
		return "", nil, ErrExpired
	}

	plainNew, hashNew, err := newTokenAndHash()
	if err != nil {
		return "", nil, err
	}

	successor := &models.RefreshToken{
		UserID:    presented.UserID,
		TokenHash: hashNew,
		ExpiresAt: now.Add(ttl),
		UserAgent: userAgent,
		IPAtIssue: ipAtIssue,
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(successor).Error; err != nil {
			return fmt.Errorf("insert successor: %w", err)
		}
		updates := map[string]any{
			"revoked_at":     now,
			"replaced_by_id": successor.ID,
		}
		if err := tx.Model(&models.RefreshToken{}).
			Where("id = ?", presented.ID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("revoke presented: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("refresh: rotate tx: %w", err)
	}
	return plainNew, successor, nil
}

// Revoke marks a single presented token as revoked. Idempotent: revoking an
// already-revoked or unknown token is a no-op (returns nil).
func Revoke(ctx context.Context, db *gorm.DB, plain string) error {
	hash := hashToken(plain)
	now := time.Now().UTC()
	res := db.WithContext(ctx).
		Model(&models.RefreshToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", hash).
		Update("revoked_at", now)
	if res.Error != nil {
		return fmt.Errorf("refresh: revoke: %w", res.Error)
	}
	return nil
}

// RevokeAllForUser is invoked during password reset / change. Marks every
// non-revoked refresh row for the user as revoked.
func RevokeAllForUser(ctx context.Context, db *gorm.DB, userID uuid.UUID) error {
	now := time.Now().UTC()
	if err := db.WithContext(ctx).
		Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now).Error; err != nil {
		return fmt.Errorf("refresh: revoke all: %w", err)
	}
	return nil
}

// revokeChainForUser is the "panic button" we trip when reuse is detected.
// Revoking the entire user's set is the safest option and matches industry
// practice (e.g. Auth0 "automatic reuse detection"): we cannot trust any
// surviving lineage once a sibling has been replayed.
func revokeChainForUser(ctx context.Context, db *gorm.DB, userID uuid.UUID, at time.Time) error {
	return db.WithContext(ctx).
		Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", at).Error
}

func newTokenAndHash() (plain string, hash []byte, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("refresh: read rand: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	h := hashToken(plain)
	return plain, h, nil
}

func hashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}
