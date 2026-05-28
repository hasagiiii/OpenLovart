// Package db owns the GORM/PostgreSQL connection and the AutoMigrate model
// registry. The package exposes a small surface so that other packages
// (handlers, services) depend on `*gorm.DB` rather than the open mechanics.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jiantaoli/openlovart/backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Pool sizing constants. These values are deliberately conservative and well
// under common single-instance Postgres limits; revisit if/when we deploy
// multiple replicas.
const (
	maxOpenConns      = 20
	maxIdleConns      = 5
	connMaxLifetime   = time.Hour
	startupPingTimeout = 5 * time.Second
)

// Open establishes a GORM connection to PostgreSQL using cfg.DatabaseURL,
// applies the documented connection pool settings, and verifies reachability
// with a bounded Ping. The returned *gorm.DB is ready for use; callers are
// responsible for invoking Close at shutdown.
func Open(ctx context.Context, cfg *config.Config, log *slog.Logger) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger: NewSlogLogger(log, cfg.LogLevel),
	}

	gormDB, err := gorm.Open(postgres.Open(cfg.DatabaseURL), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("retrieve *sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, startupPingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		// Best effort cleanup so we do not leak the half-open pool.
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	log.Info("database connected",
		slog.Int("max_open_conns", maxOpenConns),
		slog.Int("max_idle_conns", maxIdleConns),
		slog.Duration("conn_max_lifetime", connMaxLifetime),
	)
	return gormDB, nil
}

// Close shuts down the underlying *sql.DB connection pool associated with the
// supplied *gorm.DB. It is safe to call with a nil *gorm.DB (returns nil).
func Close(gormDB *gorm.DB) error {
	if gormDB == nil {
		return nil
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return fmt.Errorf("retrieve *sql.DB for close: %w", err)
	}
	return sqlDB.Close()
}
