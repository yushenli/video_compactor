package compressor

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yushenli/video_compactor/internal/logging"
	"github.com/yushenli/video_compactor/internal/settings"
)

func TestBuildScaleFilter(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		srcW, srcH int
		wantFilter string
		wantErr    bool
	}{
		// Named landscape → scale=-2:H
		{"1080p_landscape", "1080p", 1920, 1080, "scale=-2:1080", false},
		{"720p_landscape", "720p", 1920, 1080, "scale=-2:720", false},
		{"4k_landscape", "4k", 3840, 2160, "scale=-2:2160", false},
		{"2k_landscape", "2k", 1920, 1080, "scale=-2:1440", false},
		// Named portrait → scale=W:-2
		{"1080p_portrait", "1080p", 1080, 1920, "scale=1080:-2", false},
		{"720p_portrait", "720p", 720, 1280, "scale=720:-2", false},
		// Named unknown dims (0,0) → landscape fallback
		{"1080p_zero_dims", "1080p", 0, 0, "scale=-2:1080", false},
		// Raw WxH — srcW/srcH ignored
		{"1920x1080_raw", "1920x1080", 0, 0, "scale=1920:1080", false},
		{"1280x720_raw_ignores_src", "1280x720", 1000, 2000, "scale=1280:720", false},
		// Raw W*H
		{"1280x720_star", "1280*720", 0, 0, "scale=1280:720", false},
		// Errors
		{"invalid_resolution", "fullhd", 0, 0, "", true},
		{"empty_resolution", "", 0, 0, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildScaleFilter(tc.resolution, tc.srcW, tc.srcH)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("buildScaleFilter(%q, %d, %d) expected error, got nil", tc.resolution, tc.srcW, tc.srcH)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildScaleFilter(%q, %d, %d) unexpected error: %v", tc.resolution, tc.srcW, tc.srcH, err)
			}
			if got != tc.wantFilter {
				t.Errorf("buildScaleFilter(%q, %d, %d) = %q, want %q", tc.resolution, tc.srcW, tc.srcH, got, tc.wantFilter)
			}
		})
	}
}

// argsContainSeq reports whether sub appears as a contiguous subsequence within args.
func argsContainSeq(args []string, sub ...string) bool {
	for i := 0; i <= len(args)-len(sub); i++ {
		match := true
		for j, s := range sub {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestParseProbeOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantW   int
		wantH   int
		wantErr bool
	}{
		{
			name:  "landscape_no_rotation",
			input: `{"streams":[{"width":1920,"height":1080}]}`,
			wantW: 1920, wantH: 1080,
		},
		{
			name:  "landscape_rotation_0",
			input: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":0}]}]}`,
			wantW: 1920, wantH: 1080,
		},
		{
			name:  "portrait_rotation_90",
			input: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":90}]}]}`,
			wantW: 1080, wantH: 1920,
		},
		{
			name:  "portrait_rotation_neg90",
			input: `{"streams":[{"width":2688,"height":1512,"side_data_list":[{"rotation":-90}]}]}`,
			wantW: 1512, wantH: 2688,
		},
		{
			name:  "portrait_rotation_270",
			input: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":270}]}]}`,
			wantW: 1080, wantH: 1920,
		},
		{
			name:  "portrait_rotation_neg270",
			input: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":-270}]}]}`,
			wantW: 1080, wantH: 1920,
		},
		{
			name:  "rotation_180_no_swap",
			input: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":180}]}]}`,
			wantW: 1920, wantH: 1080,
		},
		{
			name:    "empty_streams",
			input:   `{"streams":[]}`,
			wantErr: true,
		},
		{
			name:  "multiple_side_data_entries",
			input: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":90}, {"rotation":90}]}]}`,
			wantW: 1080, wantH: 1920,
		},
		{
			name:    "invalid_json",
			input:   `not json`,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := parseProbeOutput([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseProbeOutput expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProbeOutput unexpected error: %v", err)
			}
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("parseProbeOutput() = (%d, %d), want (%d, %d)", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestBuildFFmpegArgsCodec(t *testing.T) {
	tests := []struct {
		name    string
		s       settings.ResolvedSettings
		wantSeq []string
	}{
		{"h265_codec", settings.ResolvedSettings{CRF: 28, Codec: "h265"}, []string{"-c:v", "libx265"}},
		{"h264_codec", settings.ResolvedSettings{CRF: 23, Codec: "h264"}, []string{"-c:v", "libx264"}},
		{"empty_codec_defaults_to_h265", settings.ResolvedSettings{CRF: 28}, []string{"-c:v", "libx265"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s, "")
			if !argsContainSeq(args, tc.wantSeq...) {
				t.Errorf("expected %v in args, got %v", tc.wantSeq, args)
			}
		})
	}
}

func TestBuildFFmpegArgsResolution(t *testing.T) {
	tests := []struct {
		name      string
		inputFile string
		s         settings.ResolvedSettings
		wantSeq   []string // non-nil: sequence must be present
		absentArg string   // non-empty: this arg must not appear
	}{
		{
			name:      "no_resolution_no_vf_flag",
			inputFile: "in.mp4",
			s:         settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			absentArg: "-vf",
		},
		{
			name:      "raw_resolution_emits_scale",
			inputFile: "in.mp4",
			s:         settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1920x1080"},
			wantSeq:   []string{"-vf", "scale=1920:1080"},
		},
		{
			name:      "raw_star_resolution_emits_scale",
			inputFile: "in.mp4",
			s:         settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1280*720"},
			wantSeq:   []string{"-vf", "scale=1280:720"},
		},
		{
			// probeVideoDimensions fails for nonexistent file → 0,0 → landscape fallback
			name:      "named_resolution_fake_file_falls_back_to_landscape",
			inputFile: "/nonexistent/fake_xyz.mp4",
			s:         settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1080p"},
			wantSeq:   []string{"-vf", "scale=-2:1080"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildFFmpegArgs(tc.inputFile, "out.mp4", tc.s, "")
			if tc.wantSeq != nil && !argsContainSeq(args, tc.wantSeq...) {
				t.Errorf("expected %v in args, got %v", tc.wantSeq, args)
			}
			if tc.absentArg != "" {
				for _, a := range args {
					if a == tc.absentArg {
						t.Errorf("expected %q to be absent, but found it in %v", tc.absentArg, args)
					}
				}
			}
		})
	}
}

func TestBuildFFmpegArgsLossless(t *testing.T) {
	tests := []struct {
		name      string
		s         settings.ResolvedSettings
		wantSeq   []string // non-nil: sequence must be present
		absentArg string   // non-empty: this arg must not appear
	}{
		{
			name:    "lossless_h265_crf0_adds_x265_params",
			s:       settings.ResolvedSettings{CRF: 0, Codec: "h265"},
			wantSeq: []string{"-x265-params", "lossless=1"},
		},
		{
			name:      "lossless_h264_crf0_no_x265_params",
			s:         settings.ResolvedSettings{CRF: 0, Codec: "h264"},
			absentArg: "-x265-params",
		},
		{
			name:      "non_lossless_h265_no_x265_params",
			s:         settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			absentArg: "-x265-params",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s, "")
			if tc.wantSeq != nil && !argsContainSeq(args, tc.wantSeq...) {
				t.Errorf("expected %v in args, got %v", tc.wantSeq, args)
			}
			if tc.absentArg != "" {
				for _, a := range args {
					if a == tc.absentArg {
						t.Errorf("expected %q to be absent, but found it in %v", tc.absentArg, args)
					}
				}
			}
		})
	}
}

func TestBuildFFmpegArgsStructure(t *testing.T) {
	tests := []struct {
		name        string
		s           settings.ResolvedSettings
		wantSeq     []string // non-nil: sequence must be present
		wantLastArg string   // non-empty: last element of args must equal this
	}{
		{
			name:    "input_follows_i_flag",
			s:       settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeq: []string{"-i", "in.mp4"},
		},
		{
			name:        "output_path_is_last_arg",
			s:           settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantLastArg: "out.mp4",
		},
		{
			name:    "audio_copy_always_present",
			s:       settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeq: []string{"-c:a", "copy"},
		},
		{
			name:    "crf_flag_present",
			s:       settings.ResolvedSettings{CRF: 23, Codec: "h265"},
			wantSeq: []string{"-crf", "23"},
		},
		{
			name:    "map_metadata_always_present",
			s:       settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeq: []string{"-map_metadata", "0"},
		},
		{
			name:    "movflags_always_present",
			s:       settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeq: []string{"-movflags", "+use_metadata_tags+faststart"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s, "")
			if tc.wantSeq != nil && !argsContainSeq(args, tc.wantSeq...) {
				t.Errorf("expected %v in args, got %v", tc.wantSeq, args)
			}
			if tc.wantLastArg != "" && (len(args) == 0 || args[len(args)-1] != tc.wantLastArg) {
				t.Errorf("expected last arg %q, got %v", tc.wantLastArg, args)
			}
		})
	}
}

func TestCopyFileTimestamp(t *testing.T) {
	t.Run("copies_mtime_to_dst", func(t *testing.T) {
		src, err := os.CreateTemp(t.TempDir(), "src-*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		src.Close()

		dst, err := os.CreateTemp(t.TempDir(), "dst-*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		dst.Close()

		// Set a known mtime on src (2 days ago).
		wantTime := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
		if err := os.Chtimes(src.Name(), wantTime, wantTime); err != nil {
			t.Fatal(err)
		}

		if err := CopyFileTimestamp(src.Name(), dst.Name()); err != nil {
			t.Fatalf("CopyFileTimestamp unexpected error: %v", err)
		}

		dstInfo, err := os.Stat(dst.Name())
		if err != nil {
			t.Fatal(err)
		}
		gotTime := dstInfo.ModTime().Truncate(time.Second)
		if !gotTime.Equal(wantTime) {
			t.Errorf("dst mtime = %v, want %v", gotTime, wantTime)
		}
	})

	t.Run("error_on_missing_src", func(t *testing.T) {
		err := CopyFileTimestamp("/nonexistent/src.mp4", "/nonexistent/dst.mp4")
		if err == nil {
			t.Fatal("expected error for missing src, got nil")
		}
	})

	t.Run("error_on_missing_dst", func(t *testing.T) {
		src, err := os.CreateTemp(t.TempDir(), "src-*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		src.Close()

		err = CopyFileTimestamp(src.Name(), "/nonexistent/dst.mp4")
		if err == nil {
			t.Fatal("expected error for missing dst, got nil")
		}
	})
}

func TestBuildVAAPIFilterChain(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		srcW, srcH int
		wantFilter string
		wantErr    bool
	}{
		{
			name:       "no_resolution_bare_upload",
			resolution: "",
			wantFilter: "format=nv12|vaapi,hwupload",
		},
		{
			name:       "raw_1920x1080",
			resolution: "1920x1080",
			wantFilter: "format=nv12|vaapi,hwupload,scale_vaapi=w=1920:h=1080",
		},
		{
			name:       "named_1080p_landscape",
			resolution: "1080p",
			srcW:       1920, srcH: 1080,
			wantFilter: "format=nv12|vaapi,hwupload,scale_vaapi=w=-2:h=1080",
		},
		{
			name:       "named_1080p_portrait",
			resolution: "1080p",
			srcW:       1080, srcH: 1920,
			wantFilter: "format=nv12|vaapi,hwupload,scale_vaapi=w=1080:h=-2",
		},
		{
			name:       "named_720p_zero_dims_landscape_fallback",
			resolution: "720p",
			srcW:       0, srcH: 0,
			wantFilter: "format=nv12|vaapi,hwupload,scale_vaapi=w=-2:h=720",
		},
		{
			name:       "invalid_resolution_returns_error",
			resolution: "invalid",
			wantErr:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildVAAPIFilterChain(tc.resolution, tc.srcW, tc.srcH)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("buildVAAPIFilterChain(%q) expected error, got nil", tc.resolution)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildVAAPIFilterChain(%q) unexpected error: %v", tc.resolution, err)
			}
			if got != tc.wantFilter {
				t.Errorf("buildVAAPIFilterChain(%q, %d, %d) = %q, want %q", tc.resolution, tc.srcW, tc.srcH, got, tc.wantFilter)
			}
		})
	}
}

func TestBuildFFmpegArgsVAAPI(t *testing.T) {
	const device = "/dev/dri/renderD128"

	tests := []struct {
		name        string
		s           settings.ResolvedSettings
		wantSeqs    [][]string // all sequences must be present
		absentArgs  []string   // none of these args may appear
		wantLastArg string
	}{
		{
			name: "vaapi_device_prepended_before_input",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeqs: [][]string{
				{"-vaapi_device", device},
				{"-i", "in.mp4"},
			},
		},
		{
			name: "h265_uses_hevc_vaapi",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeqs: [][]string{
				{"-c:v", "hevc_vaapi"},
			},
			absentArgs: []string{"libx265"},
		},
		{
			name: "h264_uses_h264_vaapi",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h264"},
			wantSeqs: [][]string{
				{"-c:v", "h264_vaapi"},
			},
			absentArgs: []string{"libx264"},
		},
		{
			name: "quality_uses_qp_not_crf",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeqs: [][]string{
				{"-qp", "28"},
			},
			absentArgs: []string{"-crf"},
		},
		{
			name: "no_resolution_bare_upload_vf",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeqs: [][]string{
				{"-vf", "format=nv12|vaapi,hwupload"},
			},
		},
		{
			name: "raw_resolution_scale_vaapi",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1920x1080"},
			wantSeqs: [][]string{
				{"-vf", "format=nv12|vaapi,hwupload,scale_vaapi=w=1920:h=1080"},
			},
		},
		{
			name: "named_resolution_nonexistent_file_landscape_fallback",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1080p"},
			wantSeqs: [][]string{
				{"-vf", "format=nv12|vaapi,hwupload,scale_vaapi=w=-2:h=1080"},
			},
		},
		{
			name: "metadata_and_audio_flags_present",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantSeqs: [][]string{
				{"-map_metadata", "0"},
				{"-movflags", "+use_metadata_tags+faststart"},
				{"-c:a", "copy"},
			},
		},
		{
			name:        "output_path_is_last_arg",
			s:           settings.ResolvedSettings{CRF: 28, Codec: "h265"},
			wantLastArg: "out.mp4",
		},
		{
			name:       "no_x265_params_lossless_with_vaapi",
			s:          settings.ResolvedSettings{CRF: 0, Codec: "h265"},
			absentArgs: []string{"-x265-params"},
		},
		{
			// An invalid resolution triggers the buildVAAPIFilterChain error path;
			// scaling is skipped and the bare upload filter chain is used.
			name: "invalid_resolution_falls_back_to_bare_upload",
			s:    settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "fullhd"},
			wantSeqs: [][]string{
				{"-vf", "format=nv12|vaapi,hwupload"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s, device)

			for _, seq := range tc.wantSeqs {
				if !argsContainSeq(args, seq...) {
					t.Errorf("expected sequence %v in args, got %v", seq, args)
				}
			}
			for _, absent := range tc.absentArgs {
				for _, a := range args {
					if a == absent {
						t.Errorf("expected %q to be absent, but found it in %v", absent, args)
					}
				}
			}
			if tc.wantLastArg != "" && (len(args) == 0 || args[len(args)-1] != tc.wantLastArg) {
				t.Errorf("expected last arg %q, got %v", tc.wantLastArg, args)
			}
		})
	}
}

func TestExecuteFFmpegNonDryRun(t *testing.T) {
	// Verify the non-dry-run path: ffmpeg is invoked for real.
	// Using "-version" causes ffmpeg to print its version and exit 0 immediately.
	t.Run("succeeds_with_version_flag", func(t *testing.T) {
		if err := ExecuteFFmpeg([]string{"-version"}, false, nil); err != nil {
			t.Errorf("expected no error running ffmpeg -version, got: %v", err)
		}
	})

	// Verify the error path: ffmpeg exits non-zero and the error is propagated.
	t.Run("returns_error_on_ffmpeg_failure", func(t *testing.T) {
		if err := ExecuteFFmpeg([]string{"-i", "nonexistent_input_file_that_does_not_exist.mp4"}, false, nil); err == nil {
			t.Error("expected error when ffmpeg fails, got nil")
		}
	})
}

func TestExecuteFFmpegDryRunLogsCommand(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	err := ExecuteFFmpeg([]string{"-i", "in.mp4", "out.mp4"}, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Dry-run ffmpeg") {
		t.Error("expected log to contain 'Dry-run ffmpeg'")
	}
	if !strings.Contains(buf.String(), "ffmpeg -i in.mp4 out.mp4") {
		t.Error("expected log to contain the ffmpeg command")
	}
}

func TestExecuteFFmpegNonDryRunLogsCommand(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	err := ExecuteFFmpeg([]string{"-version"}, false, nil)
	if err != nil {
		t.Fatalf("unexpected error running ffmpeg -version: %v", err)
	}
	if !strings.Contains(buf.String(), "Running ffmpeg") {
		t.Error("expected log to contain 'Running ffmpeg'")
	}
}

func TestBuildFFmpegArgsNamedResolutionProbeFailureLogsWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	s := settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1080p"}
	args := BuildFFmpegArgs("/nonexistent/fake_probe_test.mp4", "out.mp4", s, "")
	if !strings.Contains(buf.String(), "Unable to probe video dimensions") {
		t.Error("expected warning about probe failure")
	}
	if !argsContainSeq(args, "-vf", "scale=-2:1080") {
		t.Errorf("expected landscape fallback scale, got %v", args)
	}
}

func TestBuildVAAPIArgsLosslessCRF0LogsWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	s := settings.ResolvedSettings{CRF: 0, Codec: "h265"}
	BuildFFmpegArgs("in.mp4", "out.mp4", s, "/dev/dri/renderD128")
	if !strings.Contains(buf.String(), "Lossless encoding (CRF 0) is not supported with VA-API") {
		t.Error("expected warning about lossless not being supported with VA-API")
	}
}

func TestBuildVAAPIArgsInvalidResolutionLogsWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(logging.NewTestLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(old) })

	s := settings.ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "fullhd"}
	args := BuildFFmpegArgs("in.mp4", "out.mp4", s, "/dev/dri/renderD128")
	if !strings.Contains(buf.String(), "Invalid resolution") {
		t.Error("expected warning about invalid resolution")
	}
	if !argsContainSeq(args, "-vf", "format=nv12|vaapi,hwupload") {
		t.Errorf("expected bare upload fallback, got %v", args)
	}
}
