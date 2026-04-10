package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCompressCmdRegistersFlags(t *testing.T) {
	cmd := newCompressCmd()

	tests := []struct {
		flagName    string
		wantDefault string
	}{
		{"vaapi-device", ""},
		{"codec", ""},
		{"dry-run", "false"},
		{"ffmpeg-output", "stderr"},
	}

	for _, tc := range tests {
		t.Run(tc.flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tc.flagName)
			if flag == nil {
				t.Fatalf("expected --%s flag to be registered", tc.flagName)
			}
			if flag.DefValue != tc.wantDefault {
				t.Errorf("--%s default = %q, want %q", tc.flagName, flag.DefValue, tc.wantDefault)
			}
		})
	}
}

func TestRootCmdRegistersLogFlags(t *testing.T) {
	tests := []struct {
		flagName    string
		wantDefault string
	}{
		{"log-level", "info"},
		{"log-file", ""},
		{"error-log-file", ""},
	}

	for _, tc := range tests {
		t.Run(tc.flagName, func(t *testing.T) {
			flag := rootCmd.PersistentFlags().Lookup(tc.flagName)
			if flag == nil {
				t.Fatalf("expected --%s persistent flag to be registered", tc.flagName)
			}
			if flag.DefValue != tc.wantDefault {
				t.Errorf("--%s default = %q, want %q", tc.flagName, flag.DefValue, tc.wantDefault)
			}
		})
	}
}

func TestOpenFFmpegOutput(t *testing.T) {
	t.Run("stdout", func(t *testing.T) {
		w, cleanup, err := openFFmpegOutput("stdout")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cleanup()
		if w != os.Stdout {
			t.Error("expected os.Stdout")
		}
	})

	t.Run("stderr", func(t *testing.T) {
		w, cleanup, err := openFFmpegOutput("stderr")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cleanup()
		if w != os.Stderr {
			t.Error("expected os.Stderr")
		}
	})

	t.Run("null", func(t *testing.T) {
		w, cleanup, err := openFFmpegOutput("null")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cleanup()
		if w != io.Discard {
			t.Error("expected io.Discard for 'null'")
		}
	})

	t.Run("discard", func(t *testing.T) {
		w, cleanup, err := openFFmpegOutput("discard")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cleanup()
		if w != io.Discard {
			t.Error("expected io.Discard for 'discard'")
		}
	})

	t.Run("file_path_creates_and_writes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ffmpeg.log")
		w, cleanup, err := openFFmpegOutput(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fmt.Fprint(w, "test output")
		cleanup()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read log file: %v", err)
		}
		if string(data) != "test output" {
			t.Errorf("log file content = %q, want %q", string(data), "test output")
		}
	})

	t.Run("bad_file_path_returns_error", func(t *testing.T) {
		_, _, err := openFFmpegOutput("/nonexistent/dir/ffmpeg.log")
		if err == nil {
			t.Error("expected error for unwritable path, got nil")
		}
	})
}
