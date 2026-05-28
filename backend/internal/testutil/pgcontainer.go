// Package testutil holds shared helpers for backend integration tests.
//
// PostgresDB returns a *gorm.DB pointed at a one-shot, throwaway Postgres
// container. The container lifetime is bound to the test by t.Cleanup, and
// AutoMigrate is run with the project's full model registry so handlers /
// repos see the same schema they would in production.
//
// Each call gets its own container. We rely on the postgres module's
// in-memory mode + small image; cold-start cost is roughly 4-8 s on a
// developer laptop. Tests that need full isolation (e.g. counting all
// rows in a table) prefer one container per test; tests that only insert
// scoped data can opt into reusing a fixture by sharing a *gorm.DB across
// subtests.
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/jiantaoli/openlovart/backend/internal/db"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

// containerStartTimeout is generous to absorb the first-time `docker pull`.
const containerStartTimeout = 90 * time.Second

// PostgresDB starts a throwaway Postgres container, opens a GORM connection,
// and runs AutoMigrate over the full project model registry. The container
// and connection are torn down via t.Cleanup.
//
// On any setup failure t.Skipf("docker unavailable: %v", err) so a CI
// environment without Docker still has green tests.
func PostgresDB(t *testing.T) *gorm.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), containerStartTimeout)
	defer cancel()

	container, err := tcpg.Run(
		ctx,
		"postgres:16-alpine",
		tcpg.WithDatabase("openlovart_test"),
		tcpg.WithUsername("openlovart"),
		tcpg.WithPassword("openlovart"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(containerStartTimeout),
		),
	)
	if err != nil {
		t.Skipf("docker/postgres container unavailable: %v", err)
		return nil
	}
	t.Cleanup(func() {
		// Bound the cleanup so a hung container does not stall the test
		// binary forever.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// Reset the package-level model registry so AutoMigrate sees a clean
	// list every time. We register only what the tests need (in practice:
	// every model — they share the same FKs).
	registerAllModels()
	if err := db.AutoMigrate(gdb); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return gdb
}

// registerAllModels (re)populates the package-level model registry. We
// duplicate the cmd/server list here so a test binary does not have to
// import the main package.
func registerAllModels() {
	// db.RegisterModels appends; calling it multiple times across tests is
	// safe because AutoMigrate is idempotent. The duplicates only cost a
	// little extra CPU during the first migrate per container.
	db.RegisterModels(
		&models.User{},
		&models.Identity{},
		&models.RefreshToken{},
		&models.VerificationToken{},
		&models.PasswordResetToken{},
		&models.Project{},
		&models.CanvasElement{},
		&models.UserCredits{},
	)
}
