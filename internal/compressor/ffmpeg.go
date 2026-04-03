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
	var libCodec string
	switch s.Codec {
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
	if s.CRF == 0 && s.Codec == "h265" {
		args = append(args, "-x265-params", "lossless=1")
	}

	// Preserve all metadata from input (global tags, creation_time, GPS atoms, etc.).
	// -map_metadata 0 is ffmpeg's default but being explicit is safer and self-documenting.
	// use_metadata_tags tells the MP4 muxer to write user-defined atoms (e.g. GPS ©xyz);
	// faststart moves the moov atom to the front for streaming compatibility.
	args = append(args,
		"-map_metadata", "0",
		"-movflags", "+use_metadata_tags+faststart",
	)

	args = append(args, "-c:a", "copy", outputPath)
	return args
}

// CopyFileTimestamp copies the modification time of src to dst.
// Call this after a successful ffmpeg run so the output file retains the
// original recording date instead of the current time.
func CopyFileTimestamp(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	mtime := info.ModTime()
	if err := os.Chtimes(dst, mtime, mtime); err != nil {
		return fmt.Errorf("chtimes %s: %w", dst, err)
	}
	return nil
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

// ExecuteFFmpeg runs ffmpeg with the given arguments.
// When dryRun is true it only prints the command without executing.
func ExecuteFFmpeg(args []string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] ffmpeg %s\n", strings.Join(args, " "))
		return nil
	}

	cmd := exec.Command("ffmpeg", append([]string{"-y", "-loglevel", "warning"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return nil
}
