package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// CanvasElement stores the per-element JSON payload that makes up a Project's
// canvas. ElementData is opaque to the backend; the frontend owns its shape.
// All elements for a project are typically replaced atomically on save (see
// the `PUT /api/projects/:id/canvas-elements` handler in Phase 8).
type CanvasElement struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ProjectID uuid.UUID      `gorm:"type:uuid;not null;index"`
	ElementData datatypes.JSON `gorm:"type:jsonb;not null"`

	Project Project `gorm:"foreignKey:ProjectID;references:ID;constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (CanvasElement) TableName() string { return "canvas_elements" }
