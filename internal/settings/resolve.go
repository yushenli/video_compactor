package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yushenli/video_compactor/internal/config"
)

// qualityPresets maps named quality levels to CRF values for libx265.
var qualityPresets = map[string]int{
	"low":      32,
	"normal":   28,
	"high":     23,
	"lossless": 0,
}

// namedResolutionHeights maps shorthand names to pixel height.
var namedResolutionHeights = map[string]int{
	"720p":  720,
	"1080p": 1080,
	"1440p": 1440,
	"2k":    1440,
	"4k":    2160,
	"2160p": 2160,
}

// IsNamedResolution reports whether res is a known named resolution shorthand.
func IsNamedResolution(res string) bool {
	_, ok := namedResolutionHeights[strings.ToLower(res)]
	return ok
}

// ResolvedSettings is the fully computed settings for one video file.
type ResolvedSettings struct {
	CRF        int    // CRF value 0–51
	Resolution string // "" = keep source; otherwise verbatim from YAML (named or WxH)
	Codec      string // "h264" or "h265"
	Skip       bool
}

// ParseTags parses a comma-separated tags string into quality and resolution components.
// Tokens matching a known resolution name or WxH/W*H pattern are treated as resolution;
// everything else is treated as quality (first non-resolution token wins).
func ParseTags(tags string) (quality, resolution string) {
	for _, token := range strings.Split(tags, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if _, ok := namedResolutionHeights[lower]; ok {
			if resolution == "" {
				resolution = lower
			}
			continue
		}
		if isRawResolution(token) {
			if resolution == "" {
				resolution = token
			}
			continue
		}
		// Treat as quality
		if quality == "" {
			quality = token
		}
	}
	return
}

// isRawResolution returns true if s looks like "WxH" or "W*H".
func isRawResolution(s string) bool {
	var sep string
	if strings.Contains(s, "x") {
		sep = "x"
	} else if strings.Contains(s, "*") {
		sep = "*"
	} else {
		return false
	}
	parts := strings.SplitN(s, sep, 2)
	if len(parts) != 2 {
		return false
	}
	_, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	_, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	return err1 == nil && err2 == nil
}

// ParseResolution resolves a resolution string to (width, height) for use in an
// ffmpeg scale filter. srcW and srcH are the source video dimensions (used only
// for named shorthands to pick the shorter edge); pass 0,0 to fall back to
// landscape behaviour.
//
// Named shorthand:
//   - Portrait (srcH > srcW): shorter edge is width → (namedH, -2)
//   - Landscape / unknown:    shorter edge is height → (-2, namedH)
//
// Raw "WxH" or "W*H": srcW/srcH ignored; returns (w, h) as-is.
func ParseResolution(res string, srcW, srcH int) (width, height int, err error) {
	lower := strings.ToLower(res)
	if namedH, ok := namedResolutionHeights[lower]; ok {
		if srcH > srcW {
			// Portrait: apply named size to the width (shorter edge)
			return namedH, -2, nil
		}
		// Landscape or unknown: apply named size to the height (shorter edge)
		return -2, namedH, nil
	}
	var sep string
	if strings.Contains(res, "x") {
		sep = "x"
	} else if strings.Contains(res, "*") {
		sep = "*"
	} else {
		return 0, 0, fmt.Errorf("unrecognized resolution %q", res)
	}
	parts := strings.SplitN(res, sep, 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unrecognized resolution %q", res)
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("unrecognized resolution %q", res)
	}
	return w, h, nil
}

// qualityToCRF converts a quality string (named preset or raw integer) to a CRF value.
func qualityToCRF(quality string) (int, error) {
	lower := strings.ToLower(strings.TrimSpace(quality))
	if crf, ok := qualityPresets[lower]; ok {
		return crf, nil
	}
	crf, err := strconv.Atoi(lower)
	if err != nil {
		return 0, fmt.Errorf("unrecognized quality %q (use low/normal/high/lossless or 0-51)", quality)
	}
	if crf < 0 || crf > 51 {
		return 0, fmt.Errorf("CRF value %d out of range (0-51)", crf)
	}
	return crf, nil
}

// applyNodeSettings applies one level's settings on top of base.
// Tags are applied first (lower priority), then explicit fields override tags.
func applyNodeSettings(base ResolvedSettings, node config.Settings) (ResolvedSettings, error) {
	result := base

	// Tags: lower priority at this level
	if node.Tags != "" {
		q, r := ParseTags(node.Tags)
		if q != "" {
			crf, err := qualityToCRF(q)
			if err != nil {
				return result, err
			}
			result.CRF = crf
		}
		if r != "" {
			result.Resolution = r
		}
	}

	// Explicit fields: override tags at the same level
	if node.Quality != "" {
		crf, err := qualityToCRF(node.Quality)
		if err != nil {
			return result, err
		}
		result.CRF = crf
	}
	if node.Resolution != "" {
		result.Resolution = node.Resolution
	}
	if node.Codec != "" {
		result.Codec = node.Codec
	}
	// Skip propagates down: once true it stays true
	if node.Skip {
		result.Skip = true
	}

	return result, nil
}

// ResolveForFile computes the concrete settings for a single file.
//   - defaults: the [defaults] section from the YAML config
//   - dirChain: settings from ancestor directories, outermost first
//   - fileNode: the file's own Settings
func ResolveForFile(defaults config.Settings, dirChain []config.Settings, fileNode config.Settings) (ResolvedSettings, error) {
	base := ResolvedSettings{Codec: "h265"} // hardcoded fallback
	var err error

	base, err = applyNodeSettings(base, defaults)
	if err != nil {
		return base, err
	}
	for _, dirSettings := range dirChain {
		base, err = applyNodeSettings(base, dirSettings)
		if err != nil {
			return base, err
		}
	}
	base, err = applyNodeSettings(base, fileNode)
	if err != nil {
		return base, err
	}
	return base, nil
}
