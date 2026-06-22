// Package canvas_elements implements the per-project canvas-element
// CRUD endpoints. The semantics mirror the previous Supabase pattern: the
// frontend saves "all elements for this project" by sending the full array;
// we replace the whole set in a single transaction so consumers always see
// a coherent snapshot.
package canvas_elements

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// ElementInput is the wire shape accepted by ReplaceAllForProject. Plain
// JSON; the backend never inspects the contents.
type ElementInput struct {
	ElementData datatypes.JSON `json:"element_data"`
}

// Repo wraps the canvas-element data-access pattern.
type Repo struct{ db *gorm.DB }

// NewRepo constructs a Repo bound to db.
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// ListByProject returns every CanvasElement attached to projectID, after
// asserting that projectID is owned by userID. Returns
// gorm.ErrRecordNotFound when the project does not exist or is owned by
// someone else (single 404 either way for handler consumers).
func (r *Repo) ListByProject(ctx context.Context, projectID, userID uuid.UUID) ([]models.CanvasElement, error) {
	if err := assertOwned(ctx, r.db, projectID, userID); err != nil {
		return nil, err
	}
	var rows []models.CanvasElement
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("canvas: list: %w", err)
	}
	return rows, nil
}

// ReplaceAllForProject deletes every existing element on the project and
// inserts the supplied elements in one transaction. Ownership is enforced
// before the destructive step so an unauthorized save cannot wipe data.
func (r *Repo) ReplaceAllForProject(
	ctx context.Context,
	projectID, userID uuid.UUID,
	elements []ElementInput,
) ([]models.CanvasElement, error) {
	var inserted []models.CanvasElement
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertOwned(ctx, tx, projectID, userID); err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", projectID).
			Delete(&models.CanvasElement{}).Error; err != nil {
			return fmt.Errorf("canvas: clear: %w", err)
		}
		if len(elements) == 0 {
			return nil
		}
		rows := make([]models.CanvasElement, 0, len(elements))
		for _, e := range elements {
			rows = append(rows, models.CanvasElement{
				ProjectID:   projectID,
				ElementData: e.ElementData,
			})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("canvas: insert: %w", err)
		}
		// Bump project.updated_at so the projects-list ordering reflects
		// the most recent edit.
		if err := tx.Model(&models.Project{}).
			Where("id = ?", projectID).
			Update("updated_at", gorm.Expr("now()")).Error; err != nil {
			return fmt.Errorf("canvas: bump project updated_at: %w", err)
		}
		inserted = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inserted, nil
}

// AppendForProject inserts the supplied elements onto projectID without
// touching existing ones, after asserting ownership. It is used by the chat
// agent's image tools to add generated images to a project's canvas. Returns
// gorm.ErrRecordNotFound when the project is missing or owned by someone else.
func (r *Repo) AppendForProject(
	ctx context.Context,
	projectID, userID uuid.UUID,
	elements []ElementInput,
) ([]models.CanvasElement, error) {
	if len(elements) == 0 {
		return nil, nil
	}
	var inserted []models.CanvasElement
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := assertOwned(ctx, tx, projectID, userID); err != nil {
			return err
		}
		rows := make([]models.CanvasElement, 0, len(elements))
		for _, e := range elements {
			rows = append(rows, models.CanvasElement{
				ProjectID:   projectID,
				ElementData: e.ElementData,
			})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("canvas: append: %w", err)
		}
		if err := tx.Model(&models.Project{}).
			Where("id = ?", projectID).
			Update("updated_at", gorm.Expr("now()")).Error; err != nil {
			return fmt.Errorf("canvas: bump project updated_at: %w", err)
		}
		inserted = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inserted, nil
}

// assertOwned returns gorm.ErrRecordNotFound when projectID is not owned by
// userID. Callers map both "missing" and "wrong owner" to a single 404.
func assertOwned(ctx context.Context, tx *gorm.DB, projectID, userID uuid.UUID) error {
	var count int64
	if err := tx.WithContext(ctx).
		Model(&models.Project{}).
		Where("id = ? AND user_id = ?", projectID, userID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("canvas: ownership check: %w", err)
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// IsNotFound is a small helper so callers do not have to import gorm.
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
