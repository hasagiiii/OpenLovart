package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken records the *hash* (sha-256, 32 bytes) of every refresh token
// we issue, plus a chain pointer (ReplacedByID) that lets us detect reuse:
// presenting an already-revoked token signals theft and triggers full-chain
// revocation.
//
// Indexes:
//   - token_hash — primary lookup path during /api/auth/refresh.
//   - (user_id, revoked_at) — supports "revoke all sessions for user".
type RefreshToken struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_refresh_user_revoked,priority:1"`

	// TokenHash is sha-256 of the random plaintext token. Stored as bytea
	// (32 bytes) so we never persist the plaintext; uniqueness lets us catch
	// the (extremely unlikely) collision case as a hard error rather than a
	// silent ambiguity.
	TokenHash []byte `gorm:"type:bytea;not null;uniqueIndex"`

	ExpiresAt    time.Time
	RevokedAt    *time.Time `gorm:"index:idx_refresh_user_revoked,priority:2"`
	ReplacedByID *uuid.UUID `gorm:"type:uuid"`

	UserAgent string `gorm:"type:text"`
	IPAtIssue string `gorm:"type:text"`

	User User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
