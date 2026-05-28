// Package credits exposes the read-only `GET /api/credits` endpoint backing
// the user UI's credits widget. The row is created with Credits=1000 by the
// auth service when a new user is created (Phase 6).
package credits

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// Repo is the data-access layer.
type Repo struct{ db *gorm.DB }

// NewRepo constructs a Repo bound to db.
func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// GetByUser returns the user_credits row for userID. As a defense against
// an inconsistent state (user exists, credits row missing) we lazily insert
// a row with Credits=1000 the first time we observe the gap.
func (r *Repo) GetByUser(ctx context.Context, userID uuid.UUID) (*models.UserCredits, error) {
	var row models.UserCredits
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&row).Error
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("credits: load: %w", err)
	}
	row = models.UserCredits{UserID: userID, Credits: 1000}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("credits: lazy create: %w", err)
	}
	return &row, nil
}
