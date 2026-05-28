// Command server is the entry point for the OpenLovart Go backend.
//
// It loads configuration, initializes logging and the database connection,
// runs AutoMigrate, builds every long-lived dependency (JWT KeyStore, auth
// service, mailer, OIDC verifier, rate limiters), constructs the HTTP
// router, and blocks until SIGINT or SIGTERM triggers a graceful shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jiantaoli/openlovart/backend/internal/auth/cookies"
	"github.com/jiantaoli/openlovart/backend/internal/auth/jwt"
	"github.com/jiantaoli/openlovart/backend/internal/auth/oidc"
	"github.com/jiantaoli/openlovart/backend/internal/auth/ratelimit"
	"github.com/jiantaoli/openlovart/backend/internal/auth/service"
	"github.com/jiantaoli/openlovart/backend/internal/config"
	"github.com/jiantaoli/openlovart/backend/internal/db"
	"github.com/jiantaoli/openlovart/backend/internal/email"
	"github.com/jiantaoli/openlovart/backend/internal/httpserver"
	"github.com/jiantaoli/openlovart/backend/internal/logging"
	"github.com/jiantaoli/openlovart/backend/internal/models"
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	shutdownTimeout         = 10 * time.Second

	// loginBurst / registerBurst absorb tiny clock-skew bursts without
	// turning the limiter into a no-op. Adjust per traffic patterns.
	loginBurst    = 5
	registerBurst = 3
	forgotBurst   = 3
	oidcBurst     = 5
)

func main() {
	if err := run(); err != nil {
		// run() is responsible for logging at the appropriate level; this
		// fallback covers cases where logging itself failed to initialize.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg)
	logger.Info("backend starting",
		slog.String("env", cfg.Env),
		slog.String("addr", cfg.HTTPAddr),
		slog.String("log_level", cfg.LogLevel),
	)

	// Use a context bound to OS signals so DB open and other startup work can
	// abort if the operator hits Ctrl-C during slow boot.
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gormDB, err := db.Open(rootCtx, cfg, logger)
	if err != nil {
		logger.Error("database open failed", slog.String("err", err.Error()))
		return err
	}
	defer func() {
		if cErr := db.Close(gormDB); cErr != nil {
			logger.Error("database close failed", slog.String("err", cErr.Error()))
		}
	}()

	// Register every GORM model before AutoMigrate runs. Order is irrelevant
	// to GORM (it resolves FKs internally) but we list them in dependency
	// order for human readability: User → Identity/RefreshToken/...
	// → Project → CanvasElement → UserCredits.
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

	if err := db.AutoMigrate(gormDB); err != nil {
		logger.Error("auto migrate failed", slog.String("err", err.Error()))
		return err
	}

	// ----- Long-lived dependencies -----

	keyStore, err := jwt.NewKeyStore(cfg)
	if err != nil {
		logger.Error("jwt keystore init failed", slog.String("err", err.Error()))
		return err
	}
	if cfg.IsDev() {
		_, kid := keyStore.SigningKey()
		logger.Info("jwt keystore ready", slog.String("active_kid", kid))
	}

	mailer, err := email.NewSMTPSender(cfg)
	if err != nil {
		logger.Error("smtp init failed", slog.String("err", err.Error()))
		return err
	}

	// Google OIDC is optional in dev: when client id/secret are unset we
	// skip discovery so a developer with no Google credentials can still
	// run the rest of the stack. The OIDC start/callback handlers will
	// 500 if invoked without a verifier — we never advertise the route in
	// the UI when oidc is disabled.
	var googleVerif *oidc.GoogleVerifier
	if cfg.OIDCGoogleClientID != "" && cfg.OIDCGoogleClientSecret != "" && cfg.OIDCGoogleRedirectURL != "" {
		googleVerif, err = oidc.NewGoogleVerifier(rootCtx, cfg)
		if err != nil {
			logger.Error("google oidc init failed", slog.String("err", err.Error()))
			return err
		}
	} else {
		logger.Warn("google oidc disabled: client id/secret/redirect not set")
	}

	cookieMgr := cookies.New(cfg)
	stateSigner := oidc.NewStateSigner(cfg.AuthOIDCStateSecret)
	authSvc := service.New(gormDB, cfg, keyStore, mailer, googleVerif)

	// Per-route limiters. Burst values are conservative; tune per traffic.
	loginLimiter := ratelimit.New(float64(cfg.RateLimitLoginPerMin), loginBurst)
	registerLimiter := ratelimit.NewPerHour(float64(cfg.RateLimitRegisterPerHour), registerBurst)
	forgotLimiter := ratelimit.New(float64(cfg.RateLimitLoginPerMin), forgotBurst)
	oidcLimiter := ratelimit.New(float64(cfg.RateLimitLoginPerMin), oidcBurst)
	resendLimiter := ratelimit.NewPerHour(float64(cfg.RateLimitRegisterPerHour), forgotBurst)

	router := httpserver.New(httpserver.Deps{
		Cfg:                 cfg,
		Log:                 logger,
		DB:                  gormDB,
		KeyStore:            keyStore,
		AuthService:         authSvc,
		Cookies:             cookieMgr,
		GoogleVerif:         googleVerif,
		OIDCSigner:          stateSigner,
		LoginLimiter:        loginLimiter,
		RegisterLimiter:     registerLimiter,
		ForgotLimiter:       forgotLimiter,
		OIDCCallbackLimiter: oidcLimiter,
		VerifyResendLimiter: resendLimiter,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: serverReadHeaderTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.String("addr", cfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			logger.Error("http server failed", slog.String("err", err.Error()))
			return err
		}
		return nil
	case <-rootCtx.Done():
		logger.Info("shutdown signal received, stopping server")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("err", err.Error()))
		return err
	}
	logger.Info("backend stopped cleanly")
	return nil
}
