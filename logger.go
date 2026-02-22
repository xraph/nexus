package nexus

import (
	"log/slog"
	"os"
	"strings"
)

// Logger is a minimal structured logger interface.
// Compatible with slog, zap, Forge logger, etc.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// slogAdapter wraps *slog.Logger to implement Logger.
type slogAdapter struct {
	l *slog.Logger
}

// NewSlogLogger wraps a *slog.Logger as a nexus Logger.
func NewSlogLogger(l *slog.Logger) Logger {
	return &slogAdapter{l: l}
}

func (a *slogAdapter) Debug(msg string, args ...any) { a.l.Debug(msg, args...) }
func (a *slogAdapter) Info(msg string, args ...any)  { a.l.Info(msg, args...) }
func (a *slogAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, args...) }
func (a *slogAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }

// NewDefaultLogger creates a slog-based Logger with the given level.
func NewDefaultLogger(level string) Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	return &slogAdapter{l: slog.New(h)}
}

// noopLogger is a Logger that discards all output.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// NewNoopLogger returns a Logger that discards all output.
func NewNoopLogger() Logger { return noopLogger{} }
