package models

import (
	"time"

	"github.com/google/uuid"
)

// PasswordResetToken backs the "forgot password → click link → set a new
// password" flow. The plaintext token is included in the emailed link; only
// its sha-256 hash is stored here. Successful redemption marks UsedAt and
// invalidates every refresh token belonging to the user (handled by the
// service layer).
type PasswordResetToken struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	TokenHash []byte    `gorm:"type:bytea;not null;uniqueIndex"`
	ExpiresAt time.Time
	UsedAt    *time.Time

	User User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
}

func (PasswordResetToken) TableName() string { return "password_reset_tokens" }
