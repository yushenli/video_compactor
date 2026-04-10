package compressor

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/logging"
	"github.com/yushenli/video_compactor/internal/settings"
)

var defaultConfig = config.Config{
	Defaults: config.Settings{Quality: "normal", Codec: "h265"},
}

func TestCompressedOutputPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"video.mp4", "video.compressed.mp4"},
		{"clip.mkv", "clip.compressed.mkv"},
		{"/path/to/video.mp4", "/path/to/video.compressed.mp4"},
		{"no_ext", "no_ext.compressed"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := compressedOutputPath(tc.input); got != tc.want {
				t.Errorf("compressedOutputPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBuildTaskListEmpty(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestBuildTaskListFlatSorted(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"b.mp4": {},
		"a.mp4": {},
	}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	// walkItems sorts keys, so a.mp4 < b.mp4
	if tasks[0].InputPath != filepath.Join("/root", "a.mp4") {
		t.Errorf("expected a.mp4 first, got %q", tasks[0].InputPath)
	}
	if tasks[1].InputPath != filepath.Join("/root", "b.mp4") {
		t.Errorf("expected b.mp4 second, got %q", tasks[1].InputPath)
	}
	wantOut := filepath.Join("/root", "a.compressed.mp4")
	if tasks[0].OutputPath != wantOut {
		t.Errorf("expected output %q, got %q", wantOut, tasks[0].OutputPath)
	}
}

func TestBuildTaskListFileLevelSkip(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"skip_me.mp4": {Settings: config.Settings{Skip: true}},
		"keep_me.mp4": {},
	}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].InputPath != filepath.Join("/root", "keep_me.mp4") {
		t.Errorf("expected keep_me.mp4, got %q", tasks[0].InputPath)
	}
}

func TestBuildTaskListDirSkipPropagates(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"dir": {
			Settings: config.Settings{Skip: true},
			Items: map[string]*config.ItemNode{
				"child.mp4": {},
			},
		},
		"other.mp4": {},
	}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task (only other.mp4), got %d: %v", len(tasks), tasks)
	}
	if tasks[0].InputPath != filepath.Join("/root", "other.mp4") {
		t.Errorf("expected other.mp4, got %q", tasks[0].InputPath)
	}
}

func TestBuildTaskListDirSettingsOverrideDefaults(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"dir": {
			Settings: config.Settings{Quality: "high"},
			Items: map[string]*config.ItemNode{
				"clip.mp4": {},
			},
		},
	}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Settings.CRF != 23 {
		t.Errorf("expected CRF 23 (high), got %d", tasks[0].Settings.CRF)
	}
}

func TestBuildTaskListFileSettingsOverrideDir(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"dir": {
			Settings: config.Settings{Quality: "high"},
			Items: map[string]*config.ItemNode{
				"clip.mp4": {Settings: config.Settings{Quality: "low"}},
			},
		},
	}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Settings.CRF != 32 {
		t.Errorf("expected CRF 32 (low), got %d", tasks[0].Settings.CRF)
	}
}

func TestBuildTaskListInvalidQualityError(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Settings{Codec: "h265"},
		Items: map[string]*config.ItemNode{
			"clip.mp4": {Settings: config.Settings{Quality: "totally_invalid"}},
		},
	}
	_, err := buildTaskList(cfg, "/root")
	if err == nil {
		t.Fatal("expected error for invalid quality, got nil")
	}
}

func TestBuildTaskListNestedDirCodecInheritance(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Settings{Quality: "normal", Codec: "h264"},
		Items: map[string]*config.ItemNode{
			"dir": {
				Items: map[string]*config.ItemNode{
					"subdir": {
						Items: map[string]*config.ItemNode{
							"deep.mp4": {},
						},
					},
				},
			},
		},
	}
	tasks, err := buildTaskList(cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Settings.Codec != "h264" {
		t.Errorf("expected codec h264 inherited from defaults, got %q", tasks[0].Settings.Codec)
	}
	wantPath := filepath.Join("/root", "dir", "subdir", "deep.mp4")
	if tasks[0].InputPath != wantPath {
		t.Errorf("expected input path %q, got %q", wantPath, tasks[0].InputPath)
	}
}

func TestCompressAllEmpty(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{}
	err := CompressAll(&cfg, "/root", CompressOptions{MaxJobs: 1})
	if err != nil {
		t.Fatalf("unexpected error for empty config: %v", err)
	}
}

func TestCompressAllRunsWithoutError(t *testing.T) {
	// Raw resolution avoids calling probeVideoDimensions on a nonexistent file with a warning.
	// ExecuteFFmpeg is a stub that always returns nil.
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"a.mp4": {Settings: config.Settings{Resolution: "1920x1080"}},
	}
	err := CompressAll(&cfg, "/root", CompressOptions{MaxJobs: 2, DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompressAllInvalidQualityError(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Settings{Quality: "bad_quality"},
		Items: map[string]*config.ItemNode{
			"a.mp4": {},
		},
	}
	err := CompressAll(cfg, "/root", CompressOptions{MaxJobs: 1})
	if err == nil {
		t.Fatal("expected error for invalid quality, got nil")
	}
}

func TestBuildTaskListSkipsCompletedCompressedStatus(t *testing.T) {
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
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
		// Explicit Unfinished: false — treated as successfully compressed, skipped.
		"explicit_false_unfinished.mp4": {
			CompressedStatus: &config.CompressedStatus{Unfinished: false},
		},
		"normal.mp4": {},
	}
	tasks, err := buildTaskList(&cfg, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks (unfinished + normal), got %d", len(tasks))
	}
	// Sorted alphabetically: normal.mp4, unfinished.mp4
	if tasks[0].InputPath != filepath.Join("/root", "normal.mp4") {
		t.Errorf("expected normal.mp4 first, got %q", tasks[0].InputPath)
	}
	if tasks[1].InputPath != filepath.Join("/root", "unfinished.mp4") {
		t.Errorf("expected unfinished.mp4 second, got %q", tasks[1].InputPath)
	}
}

func TestFprintTaskTable(t *testing.T) {
	tasks := []CompressTask{
		{
			InputPath:  "/videos/vacation/clip.mp4",
			OutputPath: "/videos/vacation/clip.compressed.mp4",
			Settings:   settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1080p"},
		},
		{
			InputPath:  "/videos/birthday.mov",
			OutputPath: "/videos/birthday.compressed.mov",
			Settings:   settings.ResolvedSettings{CRF: 23, Codec: "h264"},
		},
	}

	var buf bytes.Buffer
	fprintTaskTable(&buf, tasks, "")
	out := buf.String()

	// Output column should show only the base filename, not the full path.
	if strings.Contains(out, "/videos/vacation/clip.compressed.mp4") {
		t.Error("output column should contain only base filename, but found full path")
	}
	if !strings.Contains(out, "clip.compressed.mp4") {
		t.Error("expected base filename clip.compressed.mp4 in output")
	}
	if !strings.Contains(out, "birthday.compressed.mov") {
		t.Error("expected base filename birthday.compressed.mov in output")
	}

	// Input column should still show full paths.
	if !strings.Contains(out, "/videos/vacation/clip.mp4") {
		t.Error("expected full input path in table")
	}

	// Check resolution fallback for empty resolution.
	if !strings.Contains(out, "(keep)") {
		t.Error("expected (keep) for empty resolution")
	}
	if !strings.Contains(out, "1080p") {
		t.Error("expected 1080p resolution in table")
	}

	// Check codec values.
	if !strings.Contains(out, "h264") {
		t.Error("expected h264 codec in table")
	}
}

func TestFprintTaskTableDisplaysResolvedCodec(t *testing.T) {
	tasks := []CompressTask{
		{
			InputPath:  "a.mp4",
			OutputPath: "a.compressed.mp4",
			Settings:   settings.ResolvedSettings{CRF: 28, Codec: "h265"},
		},
	}

	var buf bytes.Buffer
	fprintTaskTable(&buf, tasks, "")
	if !strings.Contains(buf.String(), "h265") {
		t.Error("expected resolved codec h265 in table")
	}
}

func TestFprintTaskTableHWHeader(t *testing.T) {
	tasks := []CompressTask{
		{
			InputPath:  "a.mp4",
			OutputPath: "a.compressed.mp4",
			Settings:   settings.ResolvedSettings{CRF: 28, Codec: "h265"},
		},
	}

	t.Run("software_shows_software_header", func(t *testing.T) {
		var buf bytes.Buffer
		fprintTaskTable(&buf, tasks, "")
		if !strings.Contains(buf.String(), "(software)") {
			t.Error("expected (software) in hardware acceleration header")
		}
	})

	t.Run("vaapi_shows_device_path_in_header", func(t *testing.T) {
		var buf bytes.Buffer
		fprintTaskTable(&buf, tasks, "/dev/dri/renderD128")
		out := buf.String()
		if !strings.Contains(out, "/dev/dri/renderD128") {
			t.Error("expected VA-API device path in hardware acceleration header")
		}
		if strings.Contains(out, "(software)") {
			t.Error("expected (software) to be absent when VA-API device is set")
		}
	})
}

func TestCompressAllEmptyLogsMessage(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{}
	err := CompressAll(&cfg, "/root", CompressOptions{MaxJobs: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No files to compress") {
		t.Error("expected log to contain 'No files to compress'")
	}
}

func TestCompressAllDryRunLogsProgress(t *testing.T) {
	var logBuf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&logBuf))
	t.Cleanup(func() { slog.SetDefault(old) })

	var ffmpegBuf bytes.Buffer
	cfg := defaultConfig
	cfg.Items = map[string]*config.ItemNode{
		"a.mp4": {Settings: config.Settings{Resolution: "1920x1080"}},
	}
	err := CompressAll(&cfg, "/root", CompressOptions{
		MaxJobs:   1,
		DryRun:    true,
		FFmpegOut: &ffmpegBuf,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := logBuf.String()
	if !strings.Contains(out, "Progress") {
		t.Error("expected log to contain 'Progress'")
	}
	if !strings.Contains(out, "Dry-run ffmpeg") {
		t.Error("expected log to contain 'Dry-run ffmpeg'")
	}
}

// TestCompressAllFFmpegError covers the error propagation path when ffmpeg exits
// non-zero (invalid input file in non-dry-run mode).
func TestCompressAllFFmpegError(t *testing.T) {
	dir := t.TempDir()
	// Write a file with non-video content — ffmpeg will fail to process it.
	if err := os.WriteFile(filepath.Join(dir, "input.mp4"), []byte("not a video"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Defaults: config.Settings{Codec: "h265", Quality: "normal"},
		Items:    map[string]*config.ItemNode{"input.mp4": {}},
	}

	var ffOut bytes.Buffer
	err := CompressAll(cfg, dir, CompressOptions{
		MaxJobs:   1,
		DryRun:    false,
		FFmpegOut: &ffOut,
	})
	if err == nil {
		t.Error("expected error when ffmpeg fails on invalid input, got nil")
	}
}
