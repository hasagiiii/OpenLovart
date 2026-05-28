package models

import (
	"time"

	"github.com/google/uuid"
)

// UserCredits tracks the soft "credits" balance shown in the UI. The row is
// created with Credits=1000 in the same DB transaction that creates the User
// (handled by the auth service in Phase 6).
type UserCredits struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Credits   int       `gorm:"not null;default:1000"`
	CreatedAt time.Time
	UpdatedAt time.Time

	User User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`
}

func (UserCredits) TableName() string { return "user_credits" }
