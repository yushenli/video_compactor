package deleter

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/logging"
)

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero bytes", 0, "0 bytes"},
		{"small bytes", 512, "512 bytes"},
		{"one KB", 1024, "1 KB"},
		{"several KB", 5120, "5 KB"},
		{"one MB", 1024 * 1024, "1.0 MB"},
		{"fractional MB", 1536 * 1024, "1.5 MB"},
		{"one GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"fractional GB", 1536 * 1024 * 1024, "1.5 GB"},
		{"large GB", 10 * 1024 * 1024 * 1024, "10.0 GB"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatSize(tc.bytes)
			if got != tc.want {
				t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
			}
		})
	}
}

func TestCollectDeletableSelectsCorrectFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files on disk.
	completedPath := filepath.Join(dir, "completed.mp4")
	unfinishedPath := filepath.Join(dir, "unfinished.mp4")
	finishedPath := filepath.Join(dir, "finished.mp4")
	noStatusPath := filepath.Join(dir, "nostatus.mp4")
	for _, path := range []string{completedPath, unfinishedPath, finishedPath, noStatusPath} {
		if err := os.WriteFile(path, make([]byte, 1000), 0644); err != nil {
			t.Fatal(err)
		}
	}

	items := map[string]*config.ItemNode{
		"completed.mp4": {
			CompressedStatus: &config.CompressedStatus{
				CompressedRatio: "42%",
				BitrateOrigin:   5200,
				BitrateTarget:   2184,
			},
		},
		"unfinished.mp4": {
			CompressedStatus: &config.CompressedStatus{Unfinished: true},
		},
		"finished.mp4": {
			CompressedStatus: &config.CompressedStatus{Unfinished: false},
		},
		"nostatus.mp4": {},
	}

	var candidates []deleteCandidate
	collectDeletable(items, dir, &candidates)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Path != completedPath {
		t.Errorf("expected %s, got %s", completedPath, candidates[0].Path)
	}
	if candidates[1].Path != finishedPath {
		t.Errorf("expected %s, got %s", finishedPath, candidates[1].Path)
	}
}

func TestCollectDeletableNestedDirectories(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(subDir, "video.mp4")
	if err := os.WriteFile(filePath, make([]byte, 2000), 0644); err != nil {
		t.Fatal(err)
	}

	items := map[string]*config.ItemNode{
		"subdir": {
			Items: map[string]*config.ItemNode{
				"video.mp4": {
					CompressedStatus: &config.CompressedStatus{
						CompressedRatio: "50%",
						BitrateOrigin:   4000,
						BitrateTarget:   2000,
					},
				},
			},
		},
	}

	var candidates []deleteCandidate
	collectDeletable(items, dir, &candidates)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Path != filePath {
		t.Errorf("expected %s, got %s", filePath, candidates[0].Path)
	}
}

func TestDeleteOriginalsDryRun(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(filePath, make([]byte, 5000), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"video.mp4": {
				CompressedStatus: &config.CompressedStatus{
					CompressedRatio: "42%",
					BitrateOrigin:   5200,
					BitrateTarget:   2184,
				},
			},
		},
	}

	err := DeleteOriginals(cfg, dir, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should still exist in dry-run mode.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("file was deleted in dry-run mode")
	}
}

func TestDeleteOriginalsActualDelete(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(filePath, make([]byte, 5000), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"video.mp4": {
				CompressedStatus: &config.CompressedStatus{
					CompressedRatio: "42%",
					BitrateOrigin:   5200,
					BitrateTarget:   2184,
				},
			},
		},
	}

	err := DeleteOriginals(cfg, dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}

func TestDeleteOriginalsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"video.mp4": {},
		},
	}

	err := DeleteOriginals(cfg, dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCollectDeletableSkipsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	// Don't create the file on disk.
	items := map[string]*config.ItemNode{
		"missing.mp4": {
			CompressedStatus: &config.CompressedStatus{
				CompressedRatio: "42%",
				BitrateOrigin:   5200,
				BitrateTarget:   2184,
			},
		},
	}

	var candidates []deleteCandidate
	collectDeletable(items, dir, &candidates)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for missing files, got %d", len(candidates))
	}
}

func TestDeleteOriginalsEmptyLogsMessage(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"video.mp4": {},
		},
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	err := DeleteOriginals(cfg, dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "No files eligible for deletion") {
		t.Errorf("expected log to contain 'No files eligible for deletion', got: %s", buf.String())
	}
}

func TestDeleteOriginalsDryRunLogsFiles(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(filePath, make([]byte, 5000), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"video.mp4": {
				CompressedStatus: &config.CompressedStatus{
					CompressedRatio: "42%",
					BitrateOrigin:   5200,
					BitrateTarget:   2184,
				},
			},
		},
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	err := DeleteOriginals(cfg, dir, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Dry-run: would delete") {
		t.Errorf("expected log to contain 'Dry-run: would delete', got: %s", output)
	}
	if !strings.Contains(output, "Dry-run summary") {
		t.Errorf("expected log to contain 'Dry-run summary', got: %s", output)
	}
}

func TestDeleteOriginalsActualDeleteLogsFiles(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(filePath, make([]byte, 5000), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"video.mp4": {
				CompressedStatus: &config.CompressedStatus{
					CompressedRatio: "42%",
					BitrateOrigin:   5200,
					BitrateTarget:   2184,
				},
			},
		},
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	err := DeleteOriginals(cfg, dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Deleted file") {
		t.Errorf("expected log to contain 'Deleted file', got: %s", output)
	}
	if !strings.Contains(output, "Deletion summary") {
		t.Errorf("expected log to contain 'Deletion summary', got: %s", output)
	}
	if !strings.Contains(output, "failed=0") {
		t.Errorf("expected log to contain 'failed=0' in deletion summary, got: %s", output)
	}
}

func TestCollectDeletableSkipsMissingFilesLogsWarning(t *testing.T) {
	dir := t.TempDir()
	items := map[string]*config.ItemNode{
		"missing.mp4": {
			CompressedStatus: &config.CompressedStatus{
				CompressedRatio: "42%",
				BitrateOrigin:   5200,
				BitrateTarget:   2184,
			},
		},
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	var candidates []deleteCandidate
	collectDeletable(items, dir, &candidates)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for missing files, got %d", len(candidates))
	}

	if !strings.Contains(buf.String(), "Could not stat file") {
		t.Errorf("expected log to contain 'Could not stat file', got: %s", buf.String())
	}
}

// TestDeleteOriginalsDeleteFailure covers the os.Remove error path and the
// resulting error return from DeleteOriginals.
func TestDeleteOriginalsDeleteFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root bypasses directory write-permission checks")
	}

	dir := t.TempDir()
	// Create a read-only subdirectory so that os.Remove on files inside it fails.
	subDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(subDir, "video.mp4")
	if err := os.WriteFile(filePath, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(subDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(subDir, 0755) })

	cfg := &config.Config{
		Items: map[string]*config.ItemNode{
			"readonly": {
				Items: map[string]*config.ItemNode{
					"video.mp4": {
						CompressedStatus: &config.CompressedStatus{
							CompressedRatio: "42%",
							BitrateOrigin:   5200,
							BitrateTarget:   2184,
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	err := DeleteOriginals(cfg, dir, false)
	if err == nil {
		t.Error("expected error when file deletion fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to delete") {
		t.Errorf("expected 'failed to delete' in error message, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "failed=1") {
		t.Errorf("expected 'failed=1' in deletion summary log, got: %s", output)
	}
}
