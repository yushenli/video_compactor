package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
)

func TestIsVideoFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"video.mp4", true},
		{"clip.mkv", true},
		{"movie.mov", true},
		{"file.avi", true},
		{"recording.mpg", true},
		{"VIDEO.MP4", true},
		{"CLIP.MKV", true},
		{"image.jpg", false},
		{"document.pdf", false},
		{"archive.zip", false},
		{"noextension", false},
		{"", false},
		{".mp4", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVideoFile(tc.name); got != tc.want {
				t.Errorf("isVideoFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsCompressedFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"video.compressed.mp4", true},
		{"clip.compressed.mkv", true},
		{"video.mp4", false},
		{"video.compressed", false}, // ".compressed" is the sole extension, stem has no .compressed suffix
		{"compressed.mp4", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCompressedFile(tc.name); got != tc.want {
				t.Errorf("isCompressedFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// makeFile creates a file (and any necessary parent dirs) under the test temp dir.
func makeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// itemAtPath navigates cfg.Items using a slash-separated key path and returns the
// node, or nil if any segment along the path is missing.
func itemAtPath(items map[string]*config.ItemNode, path string) *config.ItemNode {
	parts := strings.Split(path, "/")
	cur := items
	for i, part := range parts {
		node := cur[part]
		if node == nil {
			return nil
		}
		if i == len(parts)-1 {
			return node
		}
		cur = node.Items
		if cur == nil {
			return nil
		}
	}
	return nil
}

func TestScanDirectory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		files        []string // relative slash-separated paths to create under a temp root
		root         string   // if non-empty, used as root directly (no files created)
		filter       string
		wantErr      bool
		presentPaths []string // slash-separated item paths that must be present in cfg.Items
		absentPaths  []string // slash-separated item paths that must be absent in cfg.Items
	}{
		{
			name:         "no_filter_returns_all_video_files",
			files:        []string{"a.mp4", "b.mkv", "c.jpg", "sub/d.mov"},
			presentPaths: []string{"a.mp4", "b.mkv", "sub/d.mov"},
			absentPaths:  []string{"c.jpg"},
		},
		{
			name:         "compressed_files_excluded",
			files:        []string{"a.mp4", "a.compressed.mp4"},
			presentPaths: []string{"a.mp4"},
			absentPaths:  []string{"a.compressed.mp4"},
		},
		{
			name:         "filter_by_filename",
			files:        []string{"intro.mp4", "outro.mp4", "main.mkv"},
			filter:       `intro`,
			presentPaths: []string{"intro.mp4"},
			absentPaths:  []string{"outro.mp4", "main.mkv"},
		},
		{
			name:         "filter_by_dir_prefix",
			files:        []string{"comedy/clip.mp4", "drama/scene.mp4"},
			filter:       `^comedy`,
			presentPaths: []string{"comedy/clip.mp4"},
			absentPaths:  []string{"drama"},
		},
		{
			name:         "filter_by_extension",
			files:        []string{"a.mp4", "b.mkv"},
			filter:       `\.mp4$`,
			presentPaths: []string{"a.mp4"},
			absentPaths:  []string{"b.mkv"},
		},
		{
			name:         "filter_alternation",
			files:        []string{"comedy.mp4", "intro.mp4", "drama.mp4"},
			filter:       `comedy|intro`,
			presentPaths: []string{"comedy.mp4", "intro.mp4"},
			absentPaths:  []string{"drama.mp4"},
		},
		{
			name:        "filter_no_match_returns_empty",
			files:       []string{"a.mp4"},
			filter:      `nomatch_xyz`,
			absentPaths: []string{"a.mp4"},
		},
		{
			name:    "invalid_filter_regex",
			filter:  `[invalid`,
			wantErr: true,
		},
		{
			name:    "nonexistent_root",
			root:    "/nonexistent/path/xyz_abc",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := tc.root
			if root == "" {
				root = t.TempDir()
				for _, f := range tc.files {
					makeFile(t, filepath.Join(root, filepath.FromSlash(f)))
				}
			}
			cfg, err := ScanDirectory(root, tc.filter)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, p := range tc.presentPaths {
				if itemAtPath(cfg.Items, p) == nil {
					t.Errorf("expected item at %q to be present", p)
				}
			}
			for _, p := range tc.absentPaths {
				if itemAtPath(cfg.Items, p) != nil {
					t.Errorf("expected no item at %q, but found one", p)
				}
			}
		})
	}
}

func TestInsertFileNodeExistingFileNodeBecomesDir(t *testing.T) {
	// Exercises the branch: node exists with Items==nil (file node) but
	// a subsequent insertion treats it as a directory parent.
	items := make(map[string]*config.ItemNode)
	// First: insert "foo" as a file node.
	insertFileNode(items, "foo")
	if items["foo"] == nil || items["foo"].Items != nil {
		t.Fatal("expected foo to be a file node (Items==nil)")
	}
	// Second: insert "foo/bar.mp4" — foo must be promoted to a directory node.
	insertFileNode(items, "foo/bar.mp4")
	if items["foo"].Items == nil {
		t.Fatal("expected foo to become a directory node after inserting a child")
	}
	if items["foo"].Items["bar.mp4"] == nil {
		t.Error("expected bar.mp4 under foo")
	}
}

func TestScanDirectoryDefaultsSet(t *testing.T) {
	dir := t.TempDir()

	cfg, err := ScanDirectory(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Defaults.Codec != "h265" {
		t.Errorf("expected default codec h265, got %q", cfg.Defaults.Codec)
	}
	if cfg.Defaults.Quality != "normal" {
		t.Errorf("expected default quality normal, got %q", cfg.Defaults.Quality)
	}
}
