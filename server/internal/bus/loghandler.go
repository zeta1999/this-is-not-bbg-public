package bus

import (
	"context"
	"log/slog"
)

// BusLogHandler wraps an slog.Handler and also publishes log records to a bus topic.
type BusLogHandler struct {
	inner slog.Handler
	bus   *Bus
	topic string
}

// NewBusLogHandler creates a handler that writes to both the inner handler and the bus.
func NewBusLogHandler(inner slog.Handler, b *Bus, topic string) *BusLogHandler {
	return &BusLogHandler{inner: inner, bus: b, topic: topic}
}

func (h *BusLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *BusLogHandler) Handle(ctx context.Context, r slog.Record) error {
	// Publish to bus.
	h.bus.Publish(Message{
		Topic: h.topic,
		Payload: map[string]any{
			"Level":   r.Level.String(),
			"Message": r.Message,
			"Time":    r.Time.Format("15:04:05"),
		},
	})

	// Also write to inner handler (stderr).
	return h.inner.Handle(ctx, r)
}

func (h *BusLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &BusLogHandler{inner: h.inner.WithAttrs(attrs), bus: h.bus, topic: h.topic}
}

func (h *BusLogHandler) WithGroup(name string) slog.Handler {
	return &BusLogHandler{inner: h.inner.WithGroup(name), bus: h.bus, topic: h.topic}
}
