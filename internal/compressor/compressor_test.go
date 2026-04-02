package compressor

import (
	"path/filepath"
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
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
