package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// Repo is the data-access layer for Project. Every method enforces the
// `user_id = current user` predicate at the query level so a forgotten
// check in the handler cannot leak data.
type Repo struct {
	db *gorm.DB
}

// NewRepo constructs a Repo bound to db.
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// ListByUser returns up to `limit` projects owned by userID, in
// updated_at-DESC, id-DESC order. When `cursor` is non-nil the query
// only returns rows strictly older than the cursor — the standard
// keyset-pagination predicate `(updated_at, id) < (cursor.updated, cursor.id)`.
//
// nextCursor is non-nil iff the next page might exist; we determine that by
// asking for one more row than requested and trimming.
func (r *Repo) ListByUser(
	ctx context.Context,
	userID uuid.UUID,
	limit int,
	cursor *Cursor,
) (rows []models.Project, nextCursor *Cursor, err error) {
	q := r.db.WithContext(ctx).
		Model(&models.Project{}).
		Where("user_id = ?", userID).
		Order("updated_at DESC, id DESC").
		Limit(limit + 1)

	if cursor != nil {
		// Keyset comparison. The composite index
		// (user_id, updated_at DESC, id DESC) makes this an index-only
		// range scan.
		q = q.Where(
			"(updated_at, id) < (?, ?)",
			cursor.UpdatedAt, cursor.ID,
		)
	}

	var fetched []models.Project
	if err := q.Find(&fetched).Error; err != nil {
		return nil, nil, fmt.Errorf("projects: list: %w", err)
	}

	if len(fetched) > limit {
		last := fetched[limit-1]
		nextCursor = &Cursor{UpdatedAt: last.UpdatedAt, ID: last.ID}
		fetched = fetched[:limit]
	}
	return fetched, nextCursor, nil
}

// Get fetches a single project by id, scoped to userID. Returns
// gorm.ErrRecordNotFound for both "doesn't exist" and "belongs to another
// user" so handlers respond with a single 404 either way.
func (r *Repo) Get(ctx context.Context, id, userID uuid.UUID) (*models.Project, error) {
	var p models.Project
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Create inserts a new project for userID and returns the persisted row.
func (r *Repo) Create(ctx context.Context, userID uuid.UUID, title string, thumbnail *string) (*models.Project, error) {
	p := &models.Project{
		UserID:    userID,
		Title:     title,
		Thumbnail: thumbnail,
	}
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, fmt.Errorf("projects: create: %w", err)
	}
	return p, nil
}

// UpdatePatch is the partial update payload accepted by Update. nil fields
// mean "leave alone".
type UpdatePatch struct {
	Title     *string
	Thumbnail *string
}

// Update applies a partial update to an owned project. Returns
// gorm.ErrRecordNotFound when the row does not exist or is owned by a
// different user.
func (r *Repo) Update(ctx context.Context, id, userID uuid.UUID, patch UpdatePatch) (*models.Project, error) {
	updates := map[string]any{}
	if patch.Title != nil {
		updates["title"] = *patch.Title
	}
	if patch.Thumbnail != nil {
		updates["thumbnail"] = *patch.Thumbnail
	}
	if len(updates) == 0 {
		// No-op patch — still verify ownership + return current row.
		return r.Get(ctx, id, userID)
	}
	res := r.db.WithContext(ctx).
		Model(&models.Project{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(updates)
	if res.Error != nil {
		return nil, fmt.Errorf("projects: update: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return r.Get(ctx, id, userID)
}

// Delete hard-deletes an owned project. Returns gorm.ErrRecordNotFound when
// no row matches.
func (r *Repo) Delete(ctx context.Context, id, userID uuid.UUID) error {
	res := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&models.Project{})
	if res.Error != nil {
		return fmt.Errorf("projects: delete: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ErrIs is a tiny helper so callers don't have to import gorm to check for
// not-found.
func ErrIs(err, target error) bool { return errors.Is(err, target) }
