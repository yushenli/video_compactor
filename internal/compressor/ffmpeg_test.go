package compressor

import (
	"testing"

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
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s)
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
			args := BuildFFmpegArgs(tc.inputFile, "out.mp4", tc.s)
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
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s)
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildFFmpegArgs("in.mp4", "out.mp4", tc.s)
			if tc.wantSeq != nil && !argsContainSeq(args, tc.wantSeq...) {
				t.Errorf("expected %v in args, got %v", tc.wantSeq, args)
			}
			if tc.wantLastArg != "" && (len(args) == 0 || args[len(args)-1] != tc.wantLastArg) {
				t.Errorf("expected last arg %q, got %v", tc.wantLastArg, args)
			}
		})
	}
}
