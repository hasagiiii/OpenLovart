package models

import (
	"time"

	"github.com/google/uuid"
)

// VerificationToken backs the "click the link in your email to verify your
// address" flow. The plaintext token is included in the emailed link; only
// its sha-256 hash is stored here.
type VerificationToken struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	TokenHash []byte    `gorm:"type:bytea;not null;uniqueIndex"`
	ExpiresAt time.Time
	UsedAt    *time.Time

	User User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
}

func (VerificationToken) TableName() string { return "verification_tokens" }
