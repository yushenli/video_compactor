package scanner

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/filename"
	"github.com/yushenli/video_compactor/internal/probe"
)

var videoExtensions = map[string]bool{
	".mp4": true,
	".mkv": true,
	".mov": true,
	".avi": true,
	".mpg": true,
}

// probeFunc type aliases allow tests to stub out ffprobe calls.
var (
	probeDuration = probe.VideoDuration
	probeBitrate  = probe.VideoStreamBitrate
)

// ScanDirectory walks rootDir, finds video files (skipping *.compressed.* files),
// and returns a Config whose Items tree mirrors the directory structure.
// If filterPattern is non-empty, only files whose relative path matches the regex are included.
// For each original video file, if a compressed target already exists, the scanner
// probes both files and populates a CompressedStatus on the resulting ItemNode.
func ScanDirectory(rootDir, filterPattern string) (*config.Config, error) {
	var filterRegex *regexp.Regexp
	if filterPattern != "" {
		var err error
		filterRegex, err = regexp.Compile(filterPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid --filter regex: %w", err)
		}
	}

	cfg := &config.Config{
		Defaults: config.Settings{
			Codec:   "h265",
			Quality: "normal",
		},
		Items: make(map[string]*config.ItemNode),
	}

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !isVideoFile(name) || isCompressedFile(name) {
			return nil
		}
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if filterRegex != nil && !filterRegex.MatchString(relPath) {
			return nil
		}

		cs := probeCompressedStatus(path)
		insertFileNode(cfg.Items, relPath, cs)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// probeCompressedStatus checks whether a compressed target file exists for the
// given original file and returns a CompressedStatus describing the result.
// Returns nil when no compressed target exists.
func probeCompressedStatus(originalPath string) *config.CompressedStatus {
	targetPath := filename.CompressedOutputPath(originalPath)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return nil
	}

	origDuration, err := probeDuration(originalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warning] could not probe duration for %s: %v — marking as unfinished\n", originalPath, err)
		return &config.CompressedStatus{Unfinished: true}
	}
	targetDuration, err := probeDuration(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warning] could not probe duration for %s: %v — marking as unfinished\n", targetPath, err)
		return &config.CompressedStatus{Unfinished: true}
	}

	diff := origDuration - targetDuration
	if diff < 0 {
		diff = -diff
	}
	if diff > 2*time.Second {
		return &config.CompressedStatus{Unfinished: true}
	}

	origInfo, err := os.Stat(originalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warning] could not stat %s: %v — marking as unfinished\n", originalPath, err)
		return &config.CompressedStatus{Unfinished: true}
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warning] could not stat %s: %v — marking as unfinished\n", targetPath, err)
		return &config.CompressedStatus{Unfinished: true}
	}

	origBitrate, err := probeBitrate(originalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warning] could not probe bitrate for %s: %v — marking as unfinished\n", originalPath, err)
		return &config.CompressedStatus{Unfinished: true}
	}
	targetBitrate, err := probeBitrate(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warning] could not probe bitrate for %s: %v — marking as unfinished\n", targetPath, err)
		return &config.CompressedStatus{Unfinished: true}
	}

	ratio := float64(targetInfo.Size()) / float64(origInfo.Size()) * 100
	ratioStr := fmt.Sprintf("%d%%", int(math.Round(ratio)))

	return &config.CompressedStatus{
		CompressedRatio: ratioStr,
		BitrateOrigin:   origBitrate,
		BitrateTarget:   targetBitrate,
	}
}

func isVideoFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExtensions[ext]
}

// isCompressedFile returns true if the file was already produced by this tool.
// Pattern: <stem>.compressed.<ext>
func isCompressedFile(name string) bool {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return strings.HasSuffix(stem, ".compressed")
}

// insertFileNode inserts a file at relPath into the items map,
// creating intermediate directory nodes as needed.
// If cs is non-nil, it is attached to the leaf file node.
func insertFileNode(items map[string]*config.ItemNode, relPath string, cs *config.CompressedStatus) {
	parts := strings.Split(relPath, string(filepath.Separator))
	current := items
	for i, part := range parts {
		if i == len(parts)-1 {
			// Leaf: file node
			if _, exists := current[part]; !exists {
				current[part] = &config.ItemNode{CompressedStatus: cs}
			}
			return
		}
		// Intermediate: directory node
		if _, exists := current[part]; !exists {
			current[part] = &config.ItemNode{
				Items: make(map[string]*config.ItemNode),
			}
		} else if current[part].Items == nil {
			current[part].Items = make(map[string]*config.ItemNode)
		}
		current = current[part].Items
	}
}
