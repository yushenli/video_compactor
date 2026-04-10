package logging

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// TimeFormat is the human-readable timestamp layout used by all log output.
// It includes the named timezone abbreviation and the numeric UTC offset.
const TimeFormat = "2006-01-02 15:04:05 MST (-0700)"

// replaceAttr rewrites the builtin time attribute so it uses TimeFormat.
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.Format(TimeFormat))
		}
	}
	return a
}

// SplitHandler routes log records to two underlying text handlers based on
// severity. Records at slog.LevelWarn and above are written to errHandler;
// all others are written to outHandler.
type SplitHandler struct {
	outHandler slog.Handler // INFO, DEBUG → stdout (or custom writer)
	errHandler slog.Handler // WARN, ERROR → stderr (or custom writer)
	level      slog.Level
}

// NewSplitHandler creates a SplitHandler that writes low-severity records to
// outWriter and high-severity records (>= WARN) to errWriter.
func NewSplitHandler(outWriter, errWriter io.Writer, level slog.Level) *SplitHandler {
	opts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttr,
	}
	return &SplitHandler{
		outHandler: slog.NewTextHandler(outWriter, opts),
		errHandler: slog.NewTextHandler(errWriter, opts),
		level:      level,
	}
}

func (h *SplitHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *SplitHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		return h.errHandler.Handle(ctx, r)
	}
	return h.outHandler.Handle(ctx, r)
}

func (h *SplitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SplitHandler{
		outHandler: h.outHandler.WithAttrs(attrs),
		errHandler: h.errHandler.WithAttrs(attrs),
		level:      h.level,
	}
}

func (h *SplitHandler) WithGroup(name string) slog.Handler {
	return &SplitHandler{
		outHandler: h.outHandler.WithGroup(name),
		errHandler: h.errHandler.WithGroup(name),
		level:      h.level,
	}
}
