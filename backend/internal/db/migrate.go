package db

import (
	"fmt"

	"gorm.io/gorm"
)

// registeredModels is the package-level registry of GORM models that
// AutoMigrate should manage. It is populated by RegisterModels, typically from
// `cmd/server/main.go` so the registration order is explicit and the
// dependency direction (main → models) is obvious.
var registeredModels []any

// RegisterModels appends GORM model values to the migration registry. It is
// intentionally additive: callers may register models from multiple packages
// without having to coordinate ordering.
func RegisterModels(models ...any) {
	registeredModels = append(registeredModels, models...)
}

// requiredExtensions are PostgreSQL extensions enabled exactly once before
// AutoMigrate runs. citext powers our case-insensitive `users.email` column
// (see internal/models/user.go); pgcrypto exposes `gen_random_uuid()` so the
// `default:gen_random_uuid()` GORM tag works on every primary key.
var requiredExtensions = []string{
	"citext",
	"pgcrypto",
}

// AutoMigrate enables the required PostgreSQL extensions and then runs
// gorm.AutoMigrate over the registered model set. With an empty registry the
// extension step still runs (cheap, idempotent) and AutoMigrate is skipped.
func AutoMigrate(gormDB *gorm.DB) error {
	for _, ext := range requiredExtensions {
		stmt := fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %q", ext)
		if err := gormDB.Exec(stmt).Error; err != nil {
			return fmt.Errorf("enable extension %q: %w", ext, err)
		}
	}

	if len(registeredModels) == 0 {
		return nil
	}
	if err := gormDB.AutoMigrate(registeredModels...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

// RegisteredModels returns a defensive copy of the registry. Useful for tests
// and diagnostics; not used in production code paths.
func RegisteredModels() []any {
	out := make([]any, len(registeredModels))
	copy(out, registeredModels)
	return out
}
