package compressor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yushenli/video_compactor/internal/settings"
)

// BuildFFmpegArgs constructs the ffmpeg argument list (not including the "ffmpeg" binary itself).
func BuildFFmpegArgs(inputPath, outputPath string, s settings.ResolvedSettings) []string {
	codec := s.Codec
	if codec == "" {
		codec = "h265"
	}

	var libCodec string
	switch codec {
	case "h264":
		libCodec = "libx264"
	default:
		libCodec = "libx265"
	}

	args := []string{
		"-i", inputPath,
		"-c:v", libCodec,
		"-crf", strconv.Itoa(s.CRF),
	}

	// Video filter for resolution scaling
	if s.Resolution != "" {
		srcW, srcH := 0, 0
		if settings.IsNamedResolution(s.Resolution) {
			var err error
			srcW, srcH, err = probeVideoDimensions(inputPath)
			// Log a warning to STDERR if we can't probe dimensions, since named resolution scaling may not work well without knowing orientation
			if err != nil || srcW == 0 || srcH == 0 {
				fmt.Fprintf(os.Stderr, "[warning] unable to probe video dimensions for %s, result: %dx%d, error: %v. Named resolution scaling may not work as expected\n", inputPath, srcW, srcH, err)
			}
		}
		scaleArg, err := buildScaleFilter(s.Resolution, srcW, srcH)
		if err == nil {
			args = append(args, "-vf", scaleArg)
		}
	}

	// Lossless flag: only for h265 with CRF 0
	if s.CRF == 0 && codec == "h265" {
		args = append(args, "-x265-params", "lossless=1")
	}

	args = append(args, "-c:a", "copy", outputPath)
	return args
}

// buildScaleFilter converts a resolution string to an ffmpeg scale filter value.
// srcW and srcH are the source video dimensions (0,0 = unknown/fallback).
// Named shorthand → scale=-2:H or scale=W:-2 depending on orientation.
// Raw WxH / W*H   → scale=W:H (exact dimensions, srcW/srcH ignored).
func buildScaleFilter(resolution string, srcW, srcH int) (string, error) {
	w, h, err := settings.ParseResolution(resolution, srcW, srcH)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("scale=%d:%d", w, h), nil
}

// probeVideoDimensions runs ffprobe to get the effective display width and height
// of a video file, accounting for rotation metadata in side_data.
// Returns (0, 0, err) if probing fails.
func probeVideoDimensions(filePath string) (int, int, error) {
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-show_entries", "stream_side_data=rotation",
		"-of", "json",
		filePath,
	).Output()
	if err != nil {
		return 0, 0, err
	}
	return parseProbeOutput(out)
}

// parseProbeOutput parses ffprobe JSON output and returns the effective display
// (width, height), swapping dimensions when rotation metadata indicates ±90°/±270°.
func parseProbeOutput(data []byte) (int, int, error) {
	var probe struct {
		Streams []struct {
			Width        int `json:"width"`
			Height       int `json:"height"`
			SideDataList []struct {
				Rotation int `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return 0, 0, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}
	if len(probe.Streams) == 0 {
		return 0, 0, fmt.Errorf("no video streams found")
	}

	w, h := probe.Streams[0].Width, probe.Streams[0].Height

	// Apply rotation if present: ±90° or ±270° means stored dims are transposed
	// relative to the displayed frame.
	for _, sd := range probe.Streams[0].SideDataList {
		rot := sd.Rotation % 360
		if rot < 0 {
			rot += 360
		}
		if rot == 90 || rot == 270 {
			w, h = h, w
			break
		}
	}

	return w, h, nil
}

// ExecuteFFmpeg is a Phase 1 stub: it prints the planned ffmpeg command instead of running it.
func ExecuteFFmpeg(args []string, _ bool) error {
	fmt.Printf("[preview] ffmpeg %s\n", strings.Join(args, " "))
	return nil
}
