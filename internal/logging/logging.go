package logging

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Options controls how the global slog logger is configured.
type Options struct {
	Level        string // "debug", "info", "warn", "error"
	LogFile      string // info/debug log destination file (empty = stdout)
	ErrorLogFile string // warn/error log destination file (empty = stderr)
}

// Setup configures the default slog logger according to opts.
// It returns a cleanup function that closes any opened files.
func Setup(opts Options) (cleanup func(), err error) {
	level := ParseLevel(opts.Level)

	var outWriter, errWriter io.Writer
	var closers []io.Closer

	outWriter = os.Stdout
	errWriter = os.Stderr

	if opts.LogFile != "" {
		f, ferr := os.OpenFile(opts.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr != nil {
			return nil, fmt.Errorf("open log file: %w", ferr)
		}
		outWriter = f
		closers = append(closers, f)
	}

	if opts.ErrorLogFile != "" {
		f, ferr := os.OpenFile(opts.ErrorLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr != nil {
			closeAll(closers)
			return nil, fmt.Errorf("open error log file: %w", ferr)
		}
		errWriter = f
		closers = append(closers, f)
	}

	handler := NewSplitHandler(outWriter, errWriter, level)
	slog.SetDefault(slog.New(handler))

	cleanup = func() { closeAll(closers) }
	return cleanup, nil
}

// ParseLevel converts a string level name to a slog.Level.
// Unrecognised strings default to slog.LevelInfo.
func ParseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewTestLogger creates a logger that writes all records (including Debug) to
// buf using the same human-readable time format as production. Useful in unit
// tests for capturing and asserting log output.
func NewTestLogger(buf *bytes.Buffer) *slog.Logger {
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
	})
	return slog.New(h)
}

func closeAll(closers []io.Closer) {
	for _, c := range closers {
		c.Close()
	}
}
