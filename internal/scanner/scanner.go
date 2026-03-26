package scanner

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yushenli/video_compactor/internal/config"
)

var videoExtensions = map[string]bool{
	".mp4": true,
	".mkv": true,
	".mov": true,
	".avi": true,
	".mpg": true,
}

// ScanDirectory walks rootDir, finds video files (skipping *.compressed.* files),
// and returns a Config whose Items tree mirrors the directory structure.
// If filterPattern is non-empty, only files whose relative path matches the regex are included.
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
		insertFileNode(cfg.Items, relPath)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cfg, nil
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
func insertFileNode(items map[string]*config.ItemNode, relPath string) {
	parts := strings.Split(relPath, string(filepath.Separator))
	current := items
	for i, part := range parts {
		if i == len(parts)-1 {
			// Leaf: file node
			if _, exists := current[part]; !exists {
				current[part] = &config.ItemNode{}
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
