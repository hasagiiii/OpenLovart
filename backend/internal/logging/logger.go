// Package logging wires up the standard-library `log/slog` logger used by the
// backend. The handler format depends on the environment (text for dev, JSON
// otherwise) and the level is taken from configuration.
package logging

import (
	"log/slog"
	"os"
	"strings"

	"github.com/jiantaoli/openlovart/backend/internal/config"
)

// New constructs a *slog.Logger configured according to cfg and installs it as
// the process default via slog.SetDefault so packages that log via the package
// API (`slog.Info` etc.) share the same destination.
func New(cfg *config.Config) *slog.Logger {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.IsDev() {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// parseLevel maps a textual log level to slog.Level. Unknown values fall back
// to slog.LevelInfo to match the documented default.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}
