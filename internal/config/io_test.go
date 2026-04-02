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
