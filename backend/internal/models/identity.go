package models

import (
	"time"

	"github.com/google/uuid"
)

// Identity binds an external (or internal "password") authentication factor
// to a User. The same User may have multiple Identity rows — at most one per
// provider — so logging in with Google can later be merged with the local
// password identity.
//
// Composite unique indexes (declared via GORM tags below):
//   - uq_identity_provider_subject (provider, subject) — globally unique
//     identity key used to look up an identity during sign-in (e.g. Google
//     `sub`, or the email address for the "password" provider).
//   - uq_identity_user_provider (user_id, provider) — enforces "at most one
//     identity per provider per user" so a user cannot accumulate duplicate
//     Google links.
//
// Secret holds encoded credential material (e.g. argon2id-encoded password
// string) for the "password" provider. For OIDC providers it is nil.
type Identity struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

	UserID uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:uq_identity_user_provider,priority:1"`

	Provider string `gorm:"type:varchar(32);not null;uniqueIndex:uq_identity_provider_subject,priority:1;uniqueIndex:uq_identity_user_provider,priority:2"`

	Subject string `gorm:"type:text;not null;uniqueIndex:uq_identity_provider_subject,priority:2"`

	Secret *string `gorm:"type:text"`

	// User is the inverse side of the FK. GORM uses this to emit the
	// `OnDelete:CASCADE` constraint at AutoMigrate time.
	User User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Identity) TableName() string { return "identities" }
