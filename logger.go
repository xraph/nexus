package nexus

import (
	golog "github.com/xraph/go-utils/log"
)

// Logger is a minimal structured logger interface.
// Compatible with slog, zap, Forge logger, etc.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// logAdapter wraps a go-utils log.Logger to implement the nexus Logger interface.
type logAdapter struct {
	l golog.Logger
}

// NewLogger wraps a go-utils log.Logger as a nexus Logger.
func NewLogger(l golog.Logger) Logger {
	return &logAdapter{l: l}
}

func (a *logAdapter) Debug(msg string, args ...any) { a.l.Debug(msg, toFields(args)...) }
func (a *logAdapter) Info(msg string, args ...any)  { a.l.Info(msg, toFields(args)...) }
func (a *logAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, toFields(args)...) }
func (a *logAdapter) Error(msg string, args ...any) { a.l.Error(msg, toFields(args)...) }

// toFields converts bare key-value pairs to log.Field slice.
func toFields(args []any) []golog.Field {
	var fields []golog.Field
	for i := 0; i < len(args); i++ {
		if f, ok := args[i].(golog.Field); ok {
			fields = append(fields, f)
			continue
		}
		// Treat as key-value pair
		key, ok := args[i].(string)
		if !ok || i+1 >= len(args) {
			fields = append(fields, golog.Any("unknown", args[i]))
			continue
		}
		i++
		fields = append(fields, golog.Any(key, args[i]))
	}
	return fields
}

// noopLogger is a Logger that discards all output.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// NewNoopLogger returns a Logger that discards all output.
func NewNoopLogger() Logger { return noopLogger{} }
