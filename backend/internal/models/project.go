package models

import (
	"time"

	"github.com/google/uuid"
)

// Project is a user-owned design canvas. The (user_id, updated_at DESC,
// id DESC) composite index supports cursor pagination on the projects list:
// the cursor encodes (updated_at, id) and the query reads
//
//	WHERE user_id = ? AND (updated_at, id) < (?, ?)
//	ORDER BY updated_at DESC, id DESC LIMIT ?
//
// keeping the scan to a single index range.
type Project struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_projects_user_updated_id,priority:1"`

	Title     string  `gorm:"type:text;not null"`
	Thumbnail *string `gorm:"type:text"`

	User User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
	UpdatedAt time.Time `gorm:"index:idx_projects_user_updated_id,priority:2,sort:desc"`
}

func (Project) TableName() string { return "projects" }
