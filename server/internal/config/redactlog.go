package config

import (
	"context"
	"log/slog"
	"strings"
)

// sensitiveKeys are log attribute keys that should be redacted.
var sensitiveKeys = map[string]bool{
	"password":      true,
	"api_key":       true,
	"api_secret":    true,
	"token":         true,
	"session_token": true,
	"secret":        true,
	"webhook":       true,
	"bot_token":     true,
}

// RedactHandler wraps a slog.Handler and redacts sensitive attributes.
type RedactHandler struct {
	inner slog.Handler
}

// NewRedactHandler wraps an existing handler with sensitive field redaction.
func NewRedactHandler(inner slog.Handler) *RedactHandler {
	return &RedactHandler{inner: inner}
}

func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	var redacted []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		if isSensitive(a.Key) {
			redacted = append(redacted, slog.String(a.Key, "***REDACTED***"))
		} else {
			redacted = append(redacted, a)
		}
		return true
	})

	// Create new record with redacted attrs.
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	nr.AddAttrs(redacted...)
	return h.inner.Handle(ctx, nr)
}

func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var safe []slog.Attr
	for _, a := range attrs {
		if isSensitive(a.Key) {
			safe = append(safe, slog.String(a.Key, "***REDACTED***"))
		} else {
			safe = append(safe, a)
		}
	}
	return &RedactHandler{inner: h.inner.WithAttrs(safe)}
}

func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{inner: h.inner.WithGroup(name)}
}

func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	return sensitiveKeys[lower]
}
