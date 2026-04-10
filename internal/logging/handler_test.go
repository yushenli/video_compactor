package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSplitHandlerRoutesLevels(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	handler := NewSplitHandler(&outBuf, &errBuf, slog.LevelDebug)
	logger := slog.New(handler)

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	outStr := outBuf.String()
	errStr := errBuf.String()

	// DEBUG and INFO should go to outWriter
	if !strings.Contains(outStr, "debug msg") {
		t.Error("expected debug msg in outWriter")
	}
	if !strings.Contains(outStr, "info msg") {
		t.Error("expected info msg in outWriter")
	}
	// WARN and ERROR should go to errWriter
	if !strings.Contains(errStr, "warn msg") {
		t.Error("expected warn msg in errWriter")
	}
	if !strings.Contains(errStr, "error msg") {
		t.Error("expected error msg in errWriter")
	}
	// DEBUG/INFO should NOT appear in errWriter
	if strings.Contains(errStr, "debug msg") {
		t.Error("debug msg should not appear in errWriter")
	}
	if strings.Contains(errStr, "info msg") {
		t.Error("info msg should not appear in errWriter")
	}
	// WARN/ERROR should NOT appear in outWriter
	if strings.Contains(outStr, "warn msg") {
		t.Error("warn msg should not appear in outWriter")
	}
	if strings.Contains(outStr, "error msg") {
		t.Error("error msg should not appear in outWriter")
	}
}

func TestSplitHandlerRespectsLevel(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	handler := NewSplitHandler(&outBuf, &errBuf, slog.LevelWarn)
	logger := slog.New(handler)

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")

	if outBuf.Len() > 0 {
		t.Errorf("expected no output in outWriter when level=WARN, got %q", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "warn msg") {
		t.Error("expected warn msg in errWriter")
	}
}

func TestSplitHandlerTimeFormat(t *testing.T) {
	var buf bytes.Buffer
	handler := NewSplitHandler(&buf, &bytes.Buffer{}, slog.LevelDebug)
	logger := slog.New(handler)

	logger.Info("time test")

	out := buf.String()
	// Should contain timezone offset pattern like (-0700) or (+0000)
	tzPattern := regexp.MustCompile(`\([+-]\d{4}\)`)
	if !tzPattern.MatchString(out) {
		t.Errorf("expected timezone offset matching ([+-]NNNN), got %q", out)
	}
}

func TestSplitHandlerWithAttrs(t *testing.T) {
	var outBuf bytes.Buffer
	handler := NewSplitHandler(&outBuf, &bytes.Buffer{}, slog.LevelDebug)
	logger := slog.New(handler).With("component", "test")

	logger.Info("attributed msg")

	out := outBuf.String()
	if !strings.Contains(out, "component=test") {
		t.Errorf("expected attribute in output, got %q", out)
	}
}

func TestSplitHandlerWithGroup(t *testing.T) {
	var outBuf bytes.Buffer
	handler := NewSplitHandler(&outBuf, &bytes.Buffer{}, slog.LevelDebug)
	logger := slog.New(handler).WithGroup("mygroup")

	logger.Info("grouped msg", "key", "val")

	out := outBuf.String()
	if !strings.Contains(out, "mygroup.key=val") {
		t.Errorf("expected grouped attribute in output, got %q", out)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := ParseLevel(tc.input)
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSetupDefaults(t *testing.T) {
	cleanup, err := Setup(Options{Level: "info"})
	if err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}
	defer cleanup()
	// Just verify Setup doesn't panic and returns a valid cleanup.
}

func TestSetupWithLogFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "test.log")
	cleanup, err := Setup(Options{
		Level:   "debug",
		LogFile: logFile,
	})
	if err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}
	defer cleanup()

	slog.Info("file log test")
	cleanup() // flush

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "file log test") {
		t.Errorf("expected log message in file, got %q", string(data))
	}
}

func TestSetupWithErrorLogFile(t *testing.T) {
	errFile := filepath.Join(t.TempDir(), "error.log")
	cleanup, err := Setup(Options{
		Level:        "debug",
		ErrorLogFile: errFile,
	})
	if err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}
	defer cleanup()

	slog.Warn("warning log test")
	cleanup()

	data, err := os.ReadFile(errFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "warning log test") {
		t.Errorf("expected warning in error log file, got %q", string(data))
	}
}

func TestSetupInvalidLogFile(t *testing.T) {
	_, err := Setup(Options{
		LogFile: "/nonexistent/dir/file.log",
	})
	if err == nil {
		t.Fatal("expected error for invalid log file path")
	}
}

func TestSetupInvalidErrorLogFile(t *testing.T) {
	_, err := Setup(Options{
		ErrorLogFile: "/nonexistent/dir/file.log",
	})
	if err == nil {
		t.Fatal("expected error for invalid error log file path")
	}
}

func TestNewTestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTestLogger(&buf)

	logger.Debug("debug test")
	logger.Info("info test")
	logger.Warn("warn test")

	out := buf.String()
	if !strings.Contains(out, "debug test") {
		t.Error("expected debug msg captured by test logger")
	}
	if !strings.Contains(out, "info test") {
		t.Error("expected info msg captured by test logger")
	}
	if !strings.Contains(out, "warn test") {
		t.Error("expected warn msg captured by test logger")
	}
}
