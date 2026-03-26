package compressor

import (
	"fmt"
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
			srcW, srcH, _ = probeVideoDimensions(inputPath)
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

// probeVideoDimensions runs ffprobe to get the width and height of a video file.
// Returns (0, 0, err) if probing fails.
func probeVideoDimensions(filePath string) (int, int, error) {
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=s=x:p=0",
		filePath,
	).Output()
	if err != nil {
		return 0, 0, err
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected ffprobe output: %q", string(out))
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("unexpected ffprobe output: %q", string(out))
	}
	return w, h, nil
}

// ExecuteFFmpeg is a Phase 1 stub: it prints the planned ffmpeg command instead of running it.
func ExecuteFFmpeg(args []string, _ bool) error {
	fmt.Printf("[preview] ffmpeg %s\n", strings.Join(args, " "))
	return nil
}
