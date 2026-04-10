package deleter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
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
