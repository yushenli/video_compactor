package compressor

import (
	"fmt"
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
		scaleArg, err := buildScaleFilter(s.Resolution)
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
// Named shorthand → scale=-2:<H>  (preserves aspect ratio)
// Raw WxH / W*H   → scale=<W>:<H> (exact dimensions)
func buildScaleFilter(resolution string) (string, error) {
	w, h, err := settings.ParseResolution(resolution)
	if err != nil {
		return "", err
	}
	if w == 0 {
		return fmt.Sprintf("scale=-2:%d", h), nil
	}
	return fmt.Sprintf("scale=%d:%d", w, h), nil
}

// ExecuteFFmpeg is a Phase 1 stub: it prints the planned ffmpeg command instead of running it.
func ExecuteFFmpeg(args []string, _ bool) error {
	fmt.Printf("[preview] ffmpeg %s\n", strings.Join(args, " "))
	return nil
}
