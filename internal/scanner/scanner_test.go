package scanner

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/logging"
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
	insertFileNode(items, "foo", nil)
	if items["foo"] == nil || items["foo"].Items != nil {
		t.Fatal("expected foo to be a file node (Items==nil)")
	}
	// Second: insert "foo/bar.mp4" — foo must be promoted to a directory node.
	insertFileNode(items, "foo/bar.mp4", nil)
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

func TestProbeCompressedStatusNoTarget(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	makeFile(t, origPath)

	cs := probeCompressedStatus(origPath)
	if cs != nil {
		t.Errorf("expected nil CompressedStatus when no target exists, got %+v", cs)
	}
}

func TestProbeCompressedStatusUnfinishedDurationDiff(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	// Stub probes: durations differ by more than 2 seconds.
	origDuration := probeDuration
	origBitrate := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDuration
		probeBitrate = origBitrate
	})
	probeDuration = func(path string) (time.Duration, error) {
		if path == origPath {
			return 100 * time.Second, nil
		}
		return 95 * time.Second, nil // >2s difference
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true for duration mismatch")
	}
}

func TestProbeCompressedStatusComplete(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	// Write different sizes to test ratio calculation.
	if err := os.WriteFile(origPath, make([]byte, 10000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, make([]byte, 4200), 0644); err != nil {
		t.Fatal(err)
	}

	origDuration := probeDuration
	origBitrate := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDuration
		probeBitrate = origBitrate
	})
	probeDuration = func(path string) (time.Duration, error) {
		return 60 * time.Second, nil // Same duration for both
	}
	probeBitrate = func(path string) (int, error) {
		if path == origPath {
			return 5200, nil
		}
		return 2184, nil
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if cs.Unfinished {
		t.Error("expected Unfinished=false for completed compression")
	}
	if cs.CompressedRatio != "42%" {
		t.Errorf("CompressedRatio = %q, want %q", cs.CompressedRatio, "42%")
	}
	if cs.BitrateOrigin != 5200 {
		t.Errorf("BitrateOrigin = %d, want 5200", cs.BitrateOrigin)
	}
	if cs.BitrateTarget != 2184 {
		t.Errorf("BitrateTarget = %d, want 2184", cs.BitrateTarget)
	}
}

func TestProbeCompressedStatusZeroSizeOrigin(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	// Origin is zero bytes; target has content.
	if err := os.WriteFile(origPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, make([]byte, 1000), 0644); err != nil {
		t.Fatal(err)
	}

	origDuration := probeDuration
	origBitrate := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDuration
		probeBitrate = origBitrate
	})
	probeDuration = func(path string) (time.Duration, error) {
		return 60 * time.Second, nil
	}
	probeBitrate = func(path string) (int, error) {
		return 1000, nil
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if cs.Unfinished {
		t.Error("expected Unfinished=false")
	}
	// ratio defaults to 100.0 when origin size is zero → "100%"
	if cs.CompressedRatio != "100%" {
		t.Errorf("CompressedRatio = %q, want %q", cs.CompressedRatio, "100%")
	}
}

func TestProbeCompressedStatusProbeDurationError(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	origDuration := probeDuration
	t.Cleanup(func() { probeDuration = origDuration })
	probeDuration = func(path string) (time.Duration, error) {
		return 0, fmt.Errorf("ffprobe not found")
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when probe fails")
	}
}

func TestProbeCompressedStatusTargetDurationError(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	origDuration := probeDuration
	t.Cleanup(func() { probeDuration = origDuration })
	callCount := 0
	probeDuration = func(path string) (time.Duration, error) {
		callCount++
		if callCount == 1 {
			return 60 * time.Second, nil // original succeeds
		}
		return 0, fmt.Errorf("ffprobe failed on target")
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when target duration probe fails")
	}
}

func TestProbeCompressedStatusOrigBitrateError(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	if err := os.WriteFile(origPath, make([]byte, 10000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, make([]byte, 4200), 0644); err != nil {
		t.Fatal(err)
	}

	origDuration := probeDuration
	origBitrate := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDuration
		probeBitrate = origBitrate
	})
	probeDuration = func(path string) (time.Duration, error) { return 60 * time.Second, nil }
	probeBitrate = func(path string) (int, error) {
		return 0, fmt.Errorf("bitrate probe failed")
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when orig bitrate probe fails")
	}
}

func TestProbeCompressedStatusTargetBitrateError(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	if err := os.WriteFile(origPath, make([]byte, 10000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, make([]byte, 4200), 0644); err != nil {
		t.Fatal(err)
	}

	origDuration := probeDuration
	origBitrate := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDuration
		probeBitrate = origBitrate
	})
	probeDuration = func(path string) (time.Duration, error) { return 60 * time.Second, nil }
	callCount := 0
	probeBitrate = func(path string) (int, error) {
		callCount++
		if callCount == 1 {
			return 5200, nil // original succeeds
		}
		return 0, fmt.Errorf("target bitrate probe failed")
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when target bitrate probe fails")
	}
}

func TestInsertFileNodeWithCompressedStatus(t *testing.T) {
	items := make(map[string]*config.ItemNode)
	cs := &config.CompressedStatus{CompressedRatio: "50%", BitrateOrigin: 5000, BitrateTarget: 2500}
	insertFileNode(items, "video.mp4", cs)

	node := items["video.mp4"]
	if node == nil {
		t.Fatal("expected node to exist")
	}
	if node.CompressedStatus == nil {
		t.Fatal("expected CompressedStatus to be set")
	}
	if node.CompressedStatus.CompressedRatio != "50%" {
		t.Errorf("CompressedRatio = %q, want %q", node.CompressedStatus.CompressedRatio, "50%")
	}
}

func TestProbeCompressedStatusProbeDurationErrorLogsWarning(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	origDuration := probeDuration
	t.Cleanup(func() { probeDuration = origDuration })
	probeDuration = func(path string) (time.Duration, error) {
		return 0, fmt.Errorf("ffprobe not found")
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when probe fails")
	}
	if !strings.Contains(buf.String(), "Could not probe duration") {
		t.Errorf("expected log to contain %q, got %q", "Could not probe duration", buf.String())
	}
	if !strings.Contains(buf.String(), origPath) {
		t.Errorf("expected log to reference original file %q, got %q", origPath, buf.String())
	}
}

func TestProbeCompressedStatusTargetDurationErrorLogsWarning(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	origDuration := probeDuration
	t.Cleanup(func() { probeDuration = origDuration })
	callCount := 0
	probeDuration = func(path string) (time.Duration, error) {
		callCount++
		if callCount == 1 {
			return 60 * time.Second, nil
		}
		return 0, fmt.Errorf("ffprobe failed on target")
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when target duration probe fails")
	}
	if !strings.Contains(buf.String(), "Could not probe duration") {
		t.Errorf("expected log to contain %q, got %q", "Could not probe duration", buf.String())
	}
	if !strings.Contains(buf.String(), targetPath) {
		t.Errorf("expected log to reference target file %q, got %q", targetPath, buf.String())
	}
}

func TestProbeCompressedStatusOrigBitrateErrorLogsWarning(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	if err := os.WriteFile(origPath, make([]byte, 10000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, make([]byte, 4200), 0644); err != nil {
		t.Fatal(err)
	}

	origDuration := probeDuration
	origBitrate := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDuration
		probeBitrate = origBitrate
	})
	probeDuration = func(path string) (time.Duration, error) { return 60 * time.Second, nil }
	probeBitrate = func(path string) (int, error) {
		return 0, fmt.Errorf("bitrate probe failed")
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when orig bitrate probe fails")
	}
	if !strings.Contains(buf.String(), "Could not probe bitrate") {
		t.Errorf("expected log to contain %q, got %q", "Could not probe bitrate", buf.String())
	}
}

// TestProbeCompressedStatusTargetLongerThanOriginal exercises the `diff = -diff`
// branch when the target file is reported as slightly longer than the original.
// With a sub-2s difference the probe proceeds through to bitrate comparison.
func TestProbeCompressedStatusTargetLongerThanOriginal(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	if err := os.WriteFile(origPath, make([]byte, 10000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, make([]byte, 4200), 0644); err != nil {
		t.Fatal(err)
	}

	origDurationFn := probeDuration
	origBitrateFn := probeBitrate
	t.Cleanup(func() {
		probeDuration = origDurationFn
		probeBitrate = origBitrateFn
	})
	// Target duration is 1 second longer than original → diff = -1s, abs = 1s < 2s threshold.
	probeDuration = func(path string) (time.Duration, error) {
		if path == origPath {
			return 60 * time.Second, nil
		}
		return 61 * time.Second, nil
	}
	probeBitrate = func(path string) (int, error) {
		if path == origPath {
			return 5200, nil
		}
		return 2184, nil
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if cs.Unfinished {
		t.Error("expected Unfinished=false for 1s duration difference (within threshold)")
	}
}

// TestProbeCompressedStatusOrigStatError covers the os.Stat(originalPath) error path
// by deleting the original file during the second probeDuration call.
func TestProbeCompressedStatusOrigStatError(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	origDurationFn := probeDuration
	t.Cleanup(func() { probeDuration = origDurationFn })
	callCount := 0
	probeDuration = func(path string) (time.Duration, error) {
		callCount++
		if callCount == 2 {
			// Delete the original just before os.Stat(originalPath) is called.
			os.Remove(origPath)
		}
		return 60 * time.Second, nil
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when original file stat fails")
	}
}

func TestExtractPrefixPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pattern  string
		expected string
	}{
		// Basic / no anchor
		{"empty_string", "", ""},
		{"anchor_only", "^", ""},
		{"all_literal", "^2026", "2026"},
		{"all_literal_alpha", "^abc", "abc"},
		{"spaces_literal", "^hello world", "hello world"},
		{"slashes_literal", "^2026/January/", "2026/January/"},
		{"char_class", `^202[456]`, "202[456]"},
		{"char_class_with_dotstar", `^202[456].*`, "202[456]"},
		{"multi_segment", "^before2010/200", "before2010/200"},
		{"no_anchor_word", "comedy", ""},
		{"no_anchor_escape", `\.mp4$`, ""},
		{"immediate_dotstar", "^.*", ""},
		{"immediate_dollar", "^$", ""},
		{"immediate_paren", `^(comedy\|drama)`, ""},
		{"unescaped_dot", "^abc.def", "abc"},

		// Backslash-escaped punctuation
		{"escaped_dot", `^2026\.01`, `2026\.01`},
		{"escaped_slash", `^foo\/bar`, `foo\/bar`},
		{"escaped_backslash", `^2026\\backup`, `2026\\backup`},
		{"escaped_bracket", `^foo\[bar`, `foo\[bar`},
		{"escaped_paren", `^foo\(bar`, `foo\(bar`},
		{"escaped_star", `^foo\*bar`, `foo\*bar`},
		{"escaped_plus", `^foo\+bar`, `foo\+bar`},
		{"escaped_question", `^foo\?bar`, `foo\?bar`},
		{"escaped_pipe", `^a\|b`, `a\|b`},

		// Regex shorthands (stop)
		{"shorthand_digit", `^\d+`, ""},
		{"partial_then_shorthand", `^test\d+`, "test"},
		{"shorthand_word", `^\w+_test`, ""},
		{"shorthand_space", `^log\s`, "log"},
		{"shorthand_boundary", `^foo\bbar`, "foo"},

		// Quantifiers (drop last token)
		{"star_quantifier", "^abc*", "ab"},
		{"plus_quantifier", "^abc+", "ab"},
		{"question_quantifier", "^abc?", "ab"},
		{"brace_quantifier", "^abc{2,5}", "ab"},
		{"star_all_gone", "^a*", ""},
		{"plus_all_gone", "^a+", ""},
		{"question_first_char", "^a?b", ""},

		// Mixed escape + quantifier
		{"escaped_dot_star", `^ab\.*`, "ab"},
		{"escaped_dot_plus", `^ab\.+`, "ab"},
		{"both_escaped_noq", `^ab\.\*`, `ab\.\*`},
		{"escaped_bslash_plus", `^a\\+`, "a"},

		// Edge cases
		{"double_anchor", "^^abc", ""},
		{"hyphens_literal", "^2026-04-11", "2026-04-11"},
		{"underscores_literal", "^file_name", "file_name"},
		{"uppercase", "^UPPER", "UPPER"},
		{"mixed_case_digits", "^MiXeD123", "MiXeD123"},
		{"deep_path", "^path/to/dir", "path/to/dir"},
		{"single_char", "^a", "a"},
		{"many_segments", "^a/b/c/d/e", "a/b/c/d/e"},

		// Character class handling
		{"class_in_middle", "^foo[abc]bar", "foo[abc]bar"},
		{"class_at_start", "^[abc]def", "[abc]def"},
		{"negated_class", "^foo[^abc]bar", "foo[^abc]bar"},
		{"range_in_class", "^foo[a-z]bar", "foo[a-z]bar"},
		{"escaped_bracket_in_class", `^foo[\]]bar`, `foo[\]]bar`},
		{"class_star", "^foo[abc]*", "foo"},
		{"class_plus", "^foo[abc]+", "foo"},
		{"class_question", "^foo[abc]?", "foo"},
		{"class_brace", "^foo[abc]{2}", "foo"},
		{"class_only_star", "^[abc]*", ""},
		{"class_stays_lit_drops", "^[abc]x*", "[abc]"},
		{"class_slash_lit", "^201[456]/Jan", "201[456]/Jan"},
		{"class_start_path", "^[a-z]2026/foo", "[a-z]2026/foo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractPrefixPattern(tc.pattern)
			if got != tc.expected {
				t.Errorf("extractPrefixPattern(%q) = %q, want %q", tc.pattern, got, tc.expected)
			}
		})
	}
}

func TestSplitPrefixAtSlashes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{"no_slash", "2026", nil},
		{"one_slash", "2026/Jan", []string{"2026"}},
		{"two_slashes", "a/b/c", []string{"a", "a/b"}},
		{"class_then_slash", "201[456]/Jan", []string{"201[456]"}},
		{"class_both_sides", "a[bc]/d[ef]/ghi", []string{"a[bc]", "a[bc]/d[ef]"}},
		{"trailing_slash", "2026/", []string{"2026"}},
		{"escaped_slash", `foo\/bar`, nil},
		{"class_with_slash_inside", "foo[/]bar", nil}, // '/' inside [...] is not a boundary
		{"three_levels", "x/y/z/w", []string{"x", "x/y", "x/y/z"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := splitPrefixAtSlashes(tc.pattern)
			if len(got) != len(tc.expected) {
				t.Fatalf("splitPrefixAtSlashes(%q) returned %d segments, want %d: %v", tc.pattern, len(got), len(tc.expected), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("segment[%d] = %q, want %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

func TestScanDirectoryPruning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		files             []string
		filter            string
		presentPaths      []string
		absentPaths       []string
		// neverVisitedFiles lists files inside directories that must have been pruned —
		// the WalkDir callback must never have been invoked for these paths. An absent
		// result alone does not prove pruning (the filter regex could have excluded them
		// after the walk); this field verifies the directory was never entered at all.
		neverVisitedFiles []string
	}{
		{
			name:              "prefix_2026_skips_other_years",
			files:             []string{"2024/a.mp4", "2025/b.mp4", "2026/c.mp4"},
			filter:            `^2026`,
			presentPaths:      []string{"2026/c.mp4"},
			absentPaths:       []string{"2024", "2025"},
			neverVisitedFiles: []string{"2024/a.mp4", "2025/b.mp4"},
		},
		{
			name:              "multi_segment_prefix",
			files:             []string{"after2010/a.mp4", "before2010/2001/b.mp4", "before2010/300/c.mp4"},
			filter:            `^before2010/200`,
			presentPaths:      []string{"before2010/2001/b.mp4"},
			absentPaths:       []string{"after2010", "before2010/300"},
			neverVisitedFiles: []string{"after2010/a.mp4", "before2010/300/c.mp4"},
		},
		{
			name:              "char_class_prune",
			files:             []string{"2019/a.mp4", "2020/b.mp4", "2024/c.mp4", "2026/d.mp4", "2030/e.mp4"},
			filter:            `^202[456]`,
			presentPaths:      []string{"2024/c.mp4", "2026/d.mp4"},
			absentPaths:       []string{"2019", "2020", "2030"},
			neverVisitedFiles: []string{"2019/a.mp4", "2020/b.mp4", "2030/e.mp4"},
		},
		{
			name:         "no_filter_walks_all",
			files:        []string{"2024/a.mp4", "2025/b.mp4", "2026/c.mp4"},
			filter:       "",
			presentPaths: []string{"2024/a.mp4", "2025/b.mp4", "2026/c.mp4"},
		},
		{
			// Without an anchored prefix, no pruning occurs — drama is walked but
			// its files are excluded by the filter regex after the fact.
			name:              "unanchored_filter_no_prune",
			files:             []string{"comedy/a.mp4", "drama/b.mp4"},
			filter:            `comedy`,
			presentPaths:      []string{"comedy/a.mp4"},
			absentPaths:       []string{"drama"},
			neverVisitedFiles: nil, // drama/b.mp4 IS visited; it's just filtered by regex
		},
		{
			name:              "prefix_with_slash_prune",
			files:             []string{"2026/January/a.mp4", "2026/February/b.mp4", "2025/January/c.mp4"},
			filter:            `^2026/Jan`,
			presentPaths:      []string{"2026/January/a.mp4"},
			absentPaths:       []string{"2026/February", "2025"},
			neverVisitedFiles: []string{"2025/January/c.mp4", "2026/February/b.mp4"},
		},
		{
			name:              "escaped_dot_prefix",
			files:             []string{"2026.01/a.mp4", "2026.02/b.mp4", "2026X01/c.mp4"},
			filter:            `^2026\.01`,
			presentPaths:      []string{"2026.01/a.mp4"},
			absentPaths:       []string{"2026.02", "2026X01"},
			neverVisitedFiles: []string{"2026.02/b.mp4", "2026X01/c.mp4"},
		},
		{
			name:              "root_level_files_with_prefix",
			files:             []string{"2026_clip.mp4", "subdir/2026_clip.mp4"},
			filter:            `^2026`,
			presentPaths:      []string{"2026_clip.mp4"},
			absentPaths:       []string{"subdir"},
			neverVisitedFiles: []string{"subdir/2026_clip.mp4"},
		},
		{
			// ^(2024|2026) has no extractable prefix (starts with '('), so no pruning.
			// 2025 is absent from results but was still walked into.
			name:              "alternation_no_prune",
			files:             []string{"2024/a.mp4", "2025/b.mp4", "2026/c.mp4"},
			filter:            `^(2024|2026)`,
			presentPaths:      []string{"2024/a.mp4", "2026/c.mp4"},
			absentPaths:       []string{"2025"},
			neverVisitedFiles: nil, // 2025/b.mp4 IS visited, just filtered after the fact
		},
		{
			name:              "longer_prefix_skips_shorter_dir",
			files:             []string{"2026/a.mp4", "20260101/b.mp4"},
			filter:            `^20260101`,
			presentPaths:      []string{"20260101/b.mp4"},
			absentPaths:       []string{"2026"},
			neverVisitedFiles: []string{"2026/a.mp4"},
		},
		{
			// Prefix is shorter than the directory names at the first level.
			// ^202[56] matches dirs whose names START with 2025 or 2026 (e.g. 20250102,
			// 20260304), so those must be walked into. 20240506 doesn't start with
			// 202 followed by 5 or 6, so it must be pruned entirely.
			name:              "shorter_prefix_walks_into_longer_dirs",
			files:             []string{"20240506/a.mp4", "20250102/b.mp4", "20260304/c.mp4"},
			filter:            `^202[56]`,
			presentPaths:      []string{"20250102/b.mp4", "20260304/c.mp4"},
			absentPaths:       []string{"20240506"},
			neverVisitedFiles: []string{"20240506/a.mp4"},
		},
		{
			// 2013 and 2020 are pruned outright. 2014 is an ancestor (walked into)
			// but 2014/Feb is pruned inside it.
			name:              "char_class_ancestor_prune",
			files:             []string{"2013/Jan/a.mp4", "2014/Jan/b.mp4", "2014/Feb/c.mp4", "2020/Jan/d.mp4"},
			filter:            `^201[456]/Jan`,
			presentPaths:      []string{"2014/Jan/b.mp4"},
			absentPaths:       []string{"2013", "2014/Feb", "2020"},
			neverVisitedFiles: []string{"2013/Jan/a.mp4", "2014/Feb/c.mp4", "2020/Jan/d.mp4"},
		},
		{
			// 3-level prefix produces 2 ancestor regex segments: "^a[12]/$" and "^a[12]/d1/$".
			// b1 and a1/d2 and a1/d1/g2 are pruned at different levels.
			name: "three_level_ancestor_prune",
			files: []string{
				"a1/d1/g1/clip.mp4",
				"a1/d1/g2/clip.mp4",
				"a1/d2/g1/clip.mp4",
				"a2/d1/g1/clip.mp4",
				"b1/d1/g1/clip.mp4",
			},
			filter:            `^a[12]/d1/g1`,
			presentPaths:      []string{"a1/d1/g1/clip.mp4", "a2/d1/g1/clip.mp4"},
			absentPaths:       []string{"a1/d1/g2", "a1/d2", "b1"},
			neverVisitedFiles: []string{"a1/d1/g2/clip.mp4", "a1/d2/g1/clip.mp4", "b1/d1/g1/clip.mp4"},
		},
		{
			// 3-level prefix with char classes on all levels. xa/yb/zd is pruned inside
			// an otherwise-entered subtree.
			name: "deep_multi_level_with_classes",
			files: []string{
				"xa/yb/zc/clip.mp4",
				"xa/yb/zd/clip.mp4",
				"xa/yc/zc/clip.mp4",
				"xb/yb/zc/clip.mp4",
			},
			filter:            `^x[ab]/y[bc]/zc`,
			presentPaths:      []string{"xa/yb/zc/clip.mp4", "xa/yc/zc/clip.mp4", "xb/yb/zc/clip.mp4"},
			absentPaths:       []string{"xa/yb/zd"},
			neverVisitedFiles: []string{"xa/yb/zd/clip.mp4"},
		},
	}
	for _, tc := range tests {
		// Sub-tests are sequential (not parallel) so they can safely share walkDirEntryHook.
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, f := range tc.files {
				makeFile(t, filepath.Join(root, filepath.FromSlash(f)))
			}

			// Record every path the WalkDir callback is invoked with.
			visited := make(map[string]bool)
			walkDirEntryHook = func(relPath string, isDir bool) {
				visited[relPath] = true
			}
			t.Cleanup(func() { walkDirEntryHook = nil })

			cfg, err := ScanDirectory(root, tc.filter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, p := range tc.presentPaths {
				if itemAtPath(cfg.Items, p) == nil {
					t.Errorf("result: expected item at %q to be present", p)
				}
			}
			for _, p := range tc.absentPaths {
				if itemAtPath(cfg.Items, p) != nil {
					t.Errorf("result: expected no item at %q, but found one", p)
				}
			}
			// Verify pruning: files inside pruned directories must never have been
			// handed to the WalkDir callback at all.
			for _, f := range tc.neverVisitedFiles {
				if visited[f] {
					t.Errorf("pruning: WalkDir was called for %q, but it should have been pruned", f)
				}
			}
		})
	}
}

// TestProbeCompressedStatusTargetStatError covers the os.Stat(targetPath) error path
// by deleting the target file during its duration probe (before the stat is called).
func TestProbeCompressedStatusTargetStatError(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "video.mp4")
	targetPath := filepath.Join(dir, "video.compressed.mp4")
	makeFile(t, origPath)
	makeFile(t, targetPath)

	origDurationFn := probeDuration
	t.Cleanup(func() { probeDuration = origDurationFn })
	// Delete targetPath when its duration is probed so the subsequent os.Stat(targetPath) fails.
	probeDuration = func(path string) (time.Duration, error) {
		if path == targetPath {
			os.Remove(targetPath)
		}
		return 60 * time.Second, nil
	}

	cs := probeCompressedStatus(origPath)
	if cs == nil {
		t.Fatal("expected non-nil CompressedStatus")
	}
	if !cs.Unfinished {
		t.Error("expected Unfinished=true when target file stat fails")
	}
}
