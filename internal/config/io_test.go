package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	original := &Config{
		Defaults: Settings{Quality: "normal", Codec: "h265"},
		Items: map[string]*ItemNode{
			"video.mp4": {},
			"subdir": {
				Items: map[string]*ItemNode{
					"clip.mp4": {Settings: Settings{Quality: "high"}},
				},
			},
		},
	}

	if err := SaveConfig(original, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if loaded.Defaults.Quality != original.Defaults.Quality {
		t.Errorf("Defaults.Quality = %q, want %q", loaded.Defaults.Quality, original.Defaults.Quality)
	}
	if loaded.Defaults.Codec != original.Defaults.Codec {
		t.Errorf("Defaults.Codec = %q, want %q", loaded.Defaults.Codec, original.Defaults.Codec)
	}
	if len(loaded.Items) != 2 {
		t.Errorf("Items count = %d, want 2", len(loaded.Items))
	}
	if loaded.Items["video.mp4"] == nil {
		t.Error("video.mp4 item not found")
	}
	subdir, ok := loaded.Items["subdir"]
	if !ok || subdir.Items == nil {
		t.Fatal("subdir item not found or not a directory node")
	}
	clip, ok := subdir.Items["clip.mp4"]
	if !ok {
		t.Fatal("clip.mp4 not found in subdir")
	}
	if clip.Quality != "high" {
		t.Errorf("clip.mp4 quality = %q, want %q", clip.Quality, "high")
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	t.Parallel()
	_, err := LoadConfig("/nonexistent/path/nope.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(":\tinvalid:\t[\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestSaveConfigCreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "output.yaml")

	cfg := &Config{
		Defaults: Settings{Quality: "high"},
		Items:    map[string]*ItemNode{"a.mp4": {}},
	}
	if err := SaveConfig(cfg, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "quality: high") {
		t.Errorf("expected 'quality: high' in YAML output, got:\n%s", content)
	}
	if !strings.Contains(content, "a.mp4") {
		t.Errorf("expected 'a.mp4' in YAML output, got:\n%s", content)
	}
}

func TestSaveConfigOverwritesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "overwrite.yaml")

	first := &Config{Defaults: Settings{Quality: "low"}, Items: map[string]*ItemNode{}}
	if err := SaveConfig(first, path); err != nil {
		t.Fatalf("first SaveConfig: %v", err)
	}

	second := &Config{Defaults: Settings{Quality: "high"}, Items: map[string]*ItemNode{}}
	if err := SaveConfig(second, path); err != nil {
		t.Fatalf("second SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Defaults.Quality != "high" {
		t.Errorf("expected quality 'high' after overwrite, got %q", loaded.Defaults.Quality)
	}
}

func TestSaveLoadRoundTripWithCompressedStatus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_cs.yaml")

	original := &Config{
		Defaults: Settings{Quality: "normal", Codec: "h265"},
		Items: map[string]*ItemNode{
			"completed.mp4": {
				CompressedStatus: &CompressedStatus{
					CompressedRatio: "42%",
					BitrateOrigin:   5200,
					BitrateTarget:   2184,
				},
			},
			"unfinished.mp4": {
				CompressedStatus: &CompressedStatus{Unfinished: true},
			},
			"no_status.mp4": {},
		},
	}

	if err := SaveConfig(original, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Verify completed file preserves all fields.
	completed := loaded.Items["completed.mp4"]
	if completed == nil || completed.CompressedStatus == nil {
		t.Fatal("completed.mp4 should have CompressedStatus")
	}
	if completed.CompressedStatus.Unfinished {
		t.Error("completed.mp4 should not be unfinished")
	}
	if completed.CompressedStatus.CompressedRatio != "42%" {
		t.Errorf("CompressedRatio = %q, want %q", completed.CompressedStatus.CompressedRatio, "42%")
	}
	if completed.CompressedStatus.BitrateOrigin != 5200 {
		t.Errorf("BitrateOrigin = %d, want 5200", completed.CompressedStatus.BitrateOrigin)
	}
	if completed.CompressedStatus.BitrateTarget != 2184 {
		t.Errorf("BitrateTarget = %d, want 2184", completed.CompressedStatus.BitrateTarget)
	}

	// Verify unfinished file.
	unfinished := loaded.Items["unfinished.mp4"]
	if unfinished == nil || unfinished.CompressedStatus == nil {
		t.Fatal("unfinished.mp4 should have CompressedStatus")
	}
	if !unfinished.CompressedStatus.Unfinished {
		t.Error("unfinished.mp4 should be unfinished")
	}
	if unfinished.CompressedStatus.CompressedRatio != "" {
		t.Errorf("unfinished should have no CompressedRatio, got %q", unfinished.CompressedStatus.CompressedRatio)
	}

	// Verify file without status.
	noStatus := loaded.Items["no_status.mp4"]
	if noStatus == nil {
		t.Fatal("no_status.mp4 should exist")
	}
	if noStatus.CompressedStatus != nil {
		t.Errorf("no_status.mp4 should have nil CompressedStatus, got %+v", noStatus.CompressedStatus)
	}
}

func TestCompressedStatusOmittedFromYAMLWhenNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "no_cs.yaml")

	cfg := &Config{
		Defaults: Settings{Quality: "normal"},
		Items:    map[string]*ItemNode{"video.mp4": {}},
	}
	if err := SaveConfig(cfg, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "compressed_status") {
		t.Error("YAML should not contain compressed_status when it is nil")
	}
}

func TestSaveConfigPreservesHardLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.yaml")
	hardLinkPath := filepath.Join(dir, "linked.yaml")

	if err := os.WriteFile(originalPath, []byte("defaults:\n  quality: low\nitems: {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Link(originalPath, hardLinkPath); err != nil {
		t.Fatalf("Link: %v", err)
	}

	cfg := &Config{
		Defaults: Settings{Quality: "high", Codec: "h265"},
		Items:    map[string]*ItemNode{"video.mp4": {}},
	}

	if err := SaveConfig(cfg, hardLinkPath); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	originalData, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("ReadFile(original): %v", err)
	}
	hardLinkData, err := os.ReadFile(hardLinkPath)
	if err != nil {
		t.Fatalf("ReadFile(link): %v", err)
	}

	if string(originalData) != string(hardLinkData) {
		t.Fatalf("hard-linked files should have identical contents after save\noriginal:\n%s\nlink:\n%s", originalData, hardLinkData)
	}
	if !strings.Contains(string(originalData), "quality: high") {
		t.Fatalf("expected overwritten content to be visible through the original hard link, got:\n%s", originalData)
	}
}

func TestSaveConfigErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "missing parent directory",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing", "config.yaml")
			},
		},
		{
			name: "output path is a directory",
			path: func(t *testing.T) string {
				dir := filepath.Join(t.TempDir(), "config-dir")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				return dir
			},
		},
	}

	cfg := &Config{
		Defaults: Settings{Quality: "normal", Codec: "h265"},
		Items:    map[string]*ItemNode{"video.mp4": {}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := SaveConfig(cfg, tc.path(t))
			if err == nil {
				t.Fatal("expected SaveConfig to fail, got nil")
			}
		})
	}
}

func TestSaveConfigFailsWhenTargetWriteFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "config.yaml")
	if err := os.Symlink("/dev/full", outputPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	cfg := &Config{
		Defaults: Settings{Quality: "normal", Codec: "h265"},
		Items:    map[string]*ItemNode{"video.mp4": {}},
	}

	err := SaveConfig(cfg, outputPath)
	if err == nil {
		t.Fatal("expected SaveConfig to fail when target write fails, got nil")
	}

	matches, globErr := filepath.Glob(filepath.Join(dir, "config.yaml.tmp-*"))
	if globErr != nil {
		t.Fatalf("Glob: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files should be cleaned up on failure, found %v", matches)
	}
}
