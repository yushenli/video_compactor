package probe

import (
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"time"
)

// VideoDuration returns the duration of a video file.
func VideoDuration(filePath string) (time.Duration, error) {
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "json",
		filePath,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration for %s: %w", filePath, err)
	}
	return parseDuration(out)
}

// VideoStreamBitrate returns the average bitrate of the first video stream
// in kbps, rounded to the nearest integer.
// Falls back to computing from format size and duration when the stream
// bit_rate is unavailable ("N/A" or missing).
func VideoStreamBitrate(filePath string) (int, error) {
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=bit_rate",
		"-show_entries", "format=size,duration",
		"-of", "json",
		filePath,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe bitrate for %s: %w", filePath, err)
	}
	return parseBitrate(out)
}

// parseDuration extracts the duration field from ffprobe JSON output.
func parseDuration(data []byte) (time.Duration, error) {
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if probe.Format.Duration == "" {
		return 0, fmt.Errorf("no duration found in ffprobe output")
	}
	seconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", probe.Format.Duration, err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// parseBitrate extracts the video stream bitrate from ffprobe JSON output.
// When the stream bit_rate is unavailable, it computes an approximation
// from the format-level size and duration (total file bitrate as fallback).
func parseBitrate(data []byte) (int, error) {
	var probe struct {
		Streams []struct {
			BitRate string `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			Size     string `json:"size"`
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}

	// Try stream-level bit_rate first.
	if len(probe.Streams) > 0 && probe.Streams[0].BitRate != "" && probe.Streams[0].BitRate != "N/A" {
		bps, err := strconv.ParseFloat(probe.Streams[0].BitRate, 64)
		if err != nil {
			return 0, fmt.Errorf("parse stream bit_rate %q: %w", probe.Streams[0].BitRate, err)
		}
		return int(math.Round(bps / 1000)), nil
	}

	// Fallback: compute from format size / duration.
	if probe.Format.Size == "" || probe.Format.Duration == "" {
		return 0, fmt.Errorf("no stream bit_rate and no format size/duration available")
	}
	sizeBytes, err := strconv.ParseFloat(probe.Format.Size, 64)
	if err != nil {
		return 0, fmt.Errorf("parse format size %q: %w", probe.Format.Size, err)
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("parse format duration %q: %w", probe.Format.Duration, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("invalid duration %f", duration)
	}
	bps := (sizeBytes * 8) / duration
	return int(math.Round(bps / 1000)), nil
}
