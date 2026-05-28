// Package models defines the GORM schema for OpenLovart's auth + business
// data. Every model lives in its own file for clarity; cross-model
// relationships (FKs, cascades) are declared inline via GORM struct tags.
package models

import (
	"time"

	"github.com/google/uuid"
)

// User is the canonical identity record. Authentication factors (password,
// Google OIDC, …) live on related Identity rows so the User table itself does
// not store secrets.
//
// Email is stored as PostgreSQL `citext` to make uniqueness case-insensitive
// without surface-level normalization. The `citext` extension is enabled by
// `db.AutoMigrate` before the table is created (see internal/db/migrate.go).
type User struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Email           string     `gorm:"type:citext;uniqueIndex;not null"`
	EmailVerifiedAt *time.Time `gorm:"index"`

	// LegacyClerkID lets us link a freshly-created user to a previously-known
	// Clerk subject during migration. Optional; populated only by the
	// (one-off) data import path.
	LegacyClerkID *string `gorm:"uniqueIndex"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the table name so future renames of the Go type do not
// silently rename the SQL table.
func (User) TableName() string { return "users" }
