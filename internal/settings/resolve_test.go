package settings

import (
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
)

func TestIsNamedResolution(t *testing.T) {
	tests := []struct {
		res  string
		want bool
	}{
		{"720p", true},
		{"1080p", true},
		{"1440p", true},
		{"2k", true},
		{"4k", true},
		{"2160p", true},
		{"1080P", true},
		{"4K", true},
		{"1920x1080", false},
		{"1280*720", false},
		{"", false},
		{"1080", false},
		{"hd", false},
	}
	for _, tc := range tests {
		t.Run(tc.res, func(t *testing.T) {
			if got := IsNamedResolution(tc.res); got != tc.want {
				t.Errorf("IsNamedResolution(%q) = %v, want %v", tc.res, got, tc.want)
			}
		})
	}
}

func TestIsRawResolution(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"1920x1080", true},
		{"1280x720", true},
		{"1280*720", true},
		{"1080p", false},
		{"1920", false},
		{"abcxdef", false},
		{"abc*def", false},
		{"", false},
		{"x", false},
		{"*", false},
		{"1920x", false},
	}
	for _, tc := range tests {
		t.Run(tc.s, func(t *testing.T) {
			if got := isRawResolution(tc.s); got != tc.want {
				t.Errorf("isRawResolution(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name           string
		tags           string
		wantQuality    string
		wantResolution string
	}{
		{"quality_and_1080p", "normal,1080p", "normal", "1080p"},
		{"crf_and_4k", "22,4k", "22", "4k"},
		{"quality_only", "high", "high", ""},
		{"named_res_only", "1080p", "", "1080p"},
		{"raw_res_x_only", "1920x1080", "", "1920x1080"},
		{"raw_res_star_only", "1280*720", "", "1280*720"},
		{"empty_string", "", "", ""},
		{"first_res_wins", "high,1080p,720p", "high", "1080p"},
		{"whitespace_trimmed", "  normal , 1080p  ", "normal", "1080p"},
		{"2k_shorthand", "normal,2k", "normal", "2k"},
		{"lossless_and_4k", "lossless,4k", "lossless", "4k"},
		{"raw_int_quality_with_raw_res", "18,1920x1080", "18", "1920x1080"},
		{"quality_with_star_res", "high,1920*1080", "high", "1920*1080"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, r := ParseTags(tc.tags)
			if q != tc.wantQuality {
				t.Errorf("ParseTags(%q) quality = %q, want %q", tc.tags, q, tc.wantQuality)
			}
			if r != tc.wantResolution {
				t.Errorf("ParseTags(%q) resolution = %q, want %q", tc.tags, r, tc.wantResolution)
			}
		})
	}
}

func TestParseResolution(t *testing.T) {
	tests := []struct {
		name       string
		res        string
		srcW, srcH int
		wantW      int
		wantH      int
		wantErr    bool
	}{
		// Named landscape (srcW >= srcH) → scale=-2:namedH
		{"1080p_landscape", "1080p", 1920, 1080, -2, 1080, false},
		{"720p_landscape", "720p", 1920, 1080, -2, 720, false},
		{"1440p_landscape", "1440p", 1920, 1080, -2, 1440, false},
		{"2k_landscape", "2k", 1920, 1080, -2, 1440, false},
		{"4k_landscape", "4k", 1920, 1080, -2, 2160, false},
		{"2160p_landscape", "2160p", 1920, 1080, -2, 2160, false},
		// Named portrait (srcH > srcW) → scale=namedH:-2
		{"1080p_portrait", "1080p", 1080, 1920, 1080, -2, false},
		{"720p_portrait", "720p", 720, 1280, 720, -2, false},
		{"4k_portrait", "4k", 2160, 3840, 2160, -2, false},
		// Named unknown dims (0,0) → landscape fallback
		{"1080p_unknown_dims", "1080p", 0, 0, -2, 1080, false},
		{"720p_unknown_dims", "720p", 0, 0, -2, 720, false},
		// Named case-insensitive
		{"1080P_uppercase", "1080P", 1920, 1080, -2, 1080, false},
		{"4K_uppercase", "4K", 1920, 1080, -2, 2160, false},
		// Raw WxH — srcW/srcH ignored
		{"1920x1080_raw", "1920x1080", 0, 0, 1920, 1080, false},
		{"1280x720_raw_ignores_src", "1280x720", 1000, 2000, 1280, 720, false},
		// Raw W*H
		{"1280x720_star", "1280*720", 0, 0, 1280, 720, false},
		// Errors
		{"unrecognized_string", "fullhd", 0, 0, 0, 0, true},
		{"empty_string", "", 0, 0, 0, 0, true},
		{"just_x", "x", 0, 0, 0, 0, true},
		{"missing_second_number", "1920x", 0, 0, 0, 0, true},
		{"non_numeric_x", "abcxdef", 0, 0, 0, 0, true},
		{"non_numeric_star", "abc*def", 0, 0, 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := ParseResolution(tc.res, tc.srcW, tc.srcH)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseResolution(%q, %d, %d) expected error, got nil", tc.res, tc.srcW, tc.srcH)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResolution(%q, %d, %d) unexpected error: %v", tc.res, tc.srcW, tc.srcH, err)
			}
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("ParseResolution(%q, %d, %d) = (%d, %d), want (%d, %d)",
					tc.res, tc.srcW, tc.srcH, w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestQualityToCRF(t *testing.T) {
	tests := []struct {
		quality string
		want    int
		wantErr bool
	}{
		{"low", 32, false},
		{"normal", 28, false},
		{"high", 23, false},
		{"lossless", 0, false},
		{"LOW", 32, false},
		{"High", 23, false},
		{"18", 18, false},
		{"0", 0, false},
		{"51", 51, false},
		{" 18 ", 18, false},
		{"-1", 0, true},
		{"52", 0, true},
		{"medium", 0, true},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.quality, func(t *testing.T) {
			got, err := qualityToCRF(tc.quality)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("qualityToCRF(%q) expected error, got nil", tc.quality)
				}
				return
			}
			if err != nil {
				t.Fatalf("qualityToCRF(%q) unexpected error: %v", tc.quality, err)
			}
			if got != tc.want {
				t.Errorf("qualityToCRF(%q) = %d, want %d", tc.quality, got, tc.want)
			}
		})
	}
}

func TestResolveForFile(t *testing.T) {
	tests := []struct {
		name     string
		defaults config.Settings
		dirChain []config.Settings
		fileNode config.Settings
		want     ResolvedSettings
		wantErr  bool
	}{
		{
			name: "all_empty_uses_h265_fallback",
			want: ResolvedSettings{Codec: "h265"},
		},
		{
			name:     "defaults_quality_and_codec",
			defaults: config.Settings{Quality: "normal", Codec: "h265"},
			want:     ResolvedSettings{CRF: 28, Codec: "h265"},
		},
		{
			name:     "dir_overrides_quality",
			defaults: config.Settings{Quality: "normal", Codec: "h265"},
			dirChain: []config.Settings{{Quality: "high"}},
			want:     ResolvedSettings{CRF: 23, Codec: "h265"},
		},
		{
			name:     "file_overrides_codec",
			defaults: config.Settings{Quality: "normal", Codec: "h265"},
			fileNode: config.Settings{Codec: "h264"},
			want:     ResolvedSettings{CRF: 28, Codec: "h264"},
		},
		{
			name:     "file_overrides_dir_resolution",
			defaults: config.Settings{Quality: "normal", Codec: "h265"},
			dirChain: []config.Settings{{Resolution: "1080p"}},
			fileNode: config.Settings{Resolution: "720p"},
			want:     ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "720p"},
		},
		{
			name:     "raw_crf_integer_in_file_node",
			defaults: config.Settings{Codec: "h265"},
			fileNode: config.Settings{Quality: "18"},
			want:     ResolvedSettings{CRF: 18, Codec: "h265"},
		},
		{
			name:     "tags_at_defaults_level",
			defaults: config.Settings{Tags: "normal,1080p"},
			want:     ResolvedSettings{CRF: 28, Codec: "h265", Resolution: "1080p"},
		},
		{
			name:     "explicit_quality_overrides_tags_at_same_level",
			defaults: config.Settings{Tags: "normal,1080p", Quality: "high"},
			want:     ResolvedSettings{CRF: 23, Codec: "h265", Resolution: "1080p"},
		},
		{
			name:     "skip_true_in_dir_propagates",
			defaults: config.Settings{Quality: "normal", Codec: "h265"},
			dirChain: []config.Settings{{Skip: true}},
			want:     ResolvedSettings{CRF: 28, Codec: "h265", Skip: true},
		},
		{
			name:     "skip_true_on_file_node",
			defaults: config.Settings{Quality: "normal"},
			fileNode: config.Settings{Skip: true},
			want:     ResolvedSettings{CRF: 28, Codec: "h265", Skip: true},
		},
		{
			name:     "multi_level_dir_chain_last_wins",
			defaults: config.Settings{Quality: "normal", Codec: "h265"},
			dirChain: []config.Settings{
				{Quality: "high", Resolution: "1080p"},
				{Resolution: "720p"},
			},
			fileNode: config.Settings{Codec: "h264"},
			want:     ResolvedSettings{CRF: 23, Codec: "h264", Resolution: "720p"},
		},
		{
			name:     "invalid_quality_in_defaults",
			defaults: config.Settings{Quality: "medium"},
			wantErr:  true,
		},
		{
			name:     "invalid_quality_in_dir_chain",
			defaults: config.Settings{Codec: "h265"},
			dirChain: []config.Settings{{Quality: "invalid"}},
			wantErr:  true,
		},
		{
			name:     "invalid_quality_in_file_node",
			defaults: config.Settings{Codec: "h265"},
			fileNode: config.Settings{Quality: "bad"},
			wantErr:  true,
		},
		{
			name:     "crf_above_51_returns_error",
			defaults: config.Settings{Quality: "52"},
			wantErr:  true,
		},
		{
			name:     "tags_with_invalid_quality_token",
			defaults: config.Settings{Tags: "not_a_valid_quality"},
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveForFile(tc.defaults, tc.dirChain, tc.fileNode)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveForFile() =\n  got  %+v\n  want %+v", got, tc.want)
			}
		})
	}
}
