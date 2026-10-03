package auth

import (
	"context"
	"log/slog"
)

type RedactingHandler struct {
	next slog.Handler
}

func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, Redact(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(redactAttr(attr))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		clean = append(clean, redactAttr(attr))
	}
	return &RedactingHandler{next: h.next.WithAttrs(clean)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(attr slog.Attr) slog.Attr {
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, Redact(value.String()))
	case slog.KindGroup:
		group := value.Group()
		clean := make([]any, 0, len(group))
		for _, inner := range group {
			clean = append(clean, redactAttr(inner))
		}
		return slog.Group(attr.Key, clean...)
	case slog.KindAny:
		return slog.String(attr.Key, Redact(value.String()))
	default:
		return slog.Attr{Key: attr.Key, Value: value}
	}
}
