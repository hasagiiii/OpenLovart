package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// slogGormLogger adapts gorm/logger.Interface to a *slog.Logger so that GORM
// log entries flow through the same destination, format, and level filtering
// as the rest of the backend.
type slogGormLogger struct {
	logger *slog.Logger
	level  gormlogger.LogLevel
}

// NewSlogLogger constructs a gorm/logger.Interface that forwards records to
// slog. The textual `level` mirrors the application's LOG_LEVEL value.
func NewSlogLogger(logger *slog.Logger, level string) gormlogger.Interface {
	return &slogGormLogger{
		logger: logger,
		level:  parseGormLevel(level),
	}
}

func parseGormLevel(s string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return gormlogger.Info
	case "info":
		return gormlogger.Info
	case "warn", "warning":
		return gormlogger.Warn
	case "error":
		return gormlogger.Error
	default:
		return gormlogger.Info
	}
}

// LogMode returns a copy with the given level. Required by gorm.logger.Interface.
func (l *slogGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *slogGormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Info {
		return
	}
	l.logger.InfoContext(ctx, formatGormMsg(msg, args...))
}

func (l *slogGormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Warn {
		return
	}
	l.logger.WarnContext(ctx, formatGormMsg(msg, args...))
}

func (l *slogGormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Error {
		return
	}
	l.logger.ErrorContext(ctx, formatGormMsg(msg, args...))
}

// Trace is invoked once per SQL operation. We render the SQL at debug or info
// level depending on whether an error occurred, mirroring GORM's defaults.
func (l *slogGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && !errors.Is(err, gormlogger.ErrRecordNotFound) && l.level >= gormlogger.Error:
		l.logger.ErrorContext(ctx, "gorm sql error",
			slog.String("err", err.Error()),
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	case l.level >= gormlogger.Info:
		l.logger.DebugContext(ctx, "gorm sql",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	}
}

// formatGormMsg flattens GORM's printf-style call into a single string so we
// preserve the original message without imposing a key on slog handlers.
func formatGormMsg(msg string, args ...any) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
