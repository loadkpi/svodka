// Package logger is a thin wrapper around log/slog.
//
// It intentionally exposes a small surface and writes to stderr. The bot must
// never log message content or secrets — only operational metrics.
package logger

import (
	"context"
	"log/slog"
	"os"
)

// Logger is a leveled, structured logger.
type Logger struct {
	log *slog.Logger
}

// New constructs a Logger tagged with the service name.
func New(service string) *Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	return &Logger{log: slog.New(h).With("service", service)}
}

// Info logs at info level.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.log.InfoContext(ctx, msg, args...)
}

// Warn logs at warn level.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.log.WarnContext(ctx, msg, args...)
}

// Error logs at error level.
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.log.ErrorContext(ctx, msg, args...)
}
