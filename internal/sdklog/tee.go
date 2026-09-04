package sdklog

import (
	"context"
	"log/slog"
)

// teeHandler fans out records to console and file handlers with independent levels.
type teeHandler struct {
	console slog.Handler
	file    slog.Handler
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level) || h.file.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var first error
	if h.console.Enabled(ctx, r.Level) {
		if err := h.console.Handle(ctx, r.Clone()); err != nil && first == nil {
			first = err
		}
	}
	if h.file.Enabled(ctx, r.Level) {
		if err := h.file.Handle(ctx, r.Clone()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		console: h.console.WithAttrs(attrs),
		file:    h.file.WithAttrs(attrs),
	}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		console: h.console.WithGroup(name),
		file:    h.file.WithGroup(name),
	}
}
