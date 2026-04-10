package deleter

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yushenli/video_compactor/internal/config"
)

// deleteCandidate represents a file eligible for deletion.
type deleteCandidate struct {
	Path string
	Size int64
}

// DeleteOriginals walks the config tree and deletes original video files
// that have a CompressedStatus without the Unfinished flag.
// In dryRun mode it only lists the files that would be deleted.
// Returns a non-nil error if any deletions failed.
func DeleteOriginals(cfg *config.Config, rootDir string, dryRun bool) error {
	var candidates []deleteCandidate
	collectDeletable(cfg.Items, rootDir, &candidates)

	if len(candidates) == 0 {
		slog.Info("No files eligible for deletion.")
		return nil
	}

	var totalSize int64
	var failures []string

	for _, candidate := range candidates {
		if dryRun {
			slog.Info("Dry-run: would delete",
				"path", candidate.Path, "size", FormatSize(candidate.Size))
			totalSize += candidate.Size
		} else {
			if err := os.Remove(candidate.Path); err != nil {
				slog.Error("Failed to delete file",
					"path", candidate.Path, "error", err)
				failures = append(failures, candidate.Path)
			} else {
				totalSize += candidate.Size
				slog.Info("Deleted file",
					"path", candidate.Path, "size", FormatSize(candidate.Size))
			}
		}
	}

	if dryRun {
		slog.Info("Dry-run summary",
			"files", len(candidates), "totalSize", FormatSize(totalSize))
	} else {
		slog.Info("Deletion summary",
			"deleted", len(candidates)-len(failures), "failed", len(failures), "freed", FormatSize(totalSize))
	}

	if len(failures) > 0 {
		return fmt.Errorf("failed to delete %d file(s): %s", len(failures), strings.Join(failures, ", "))
	}
	return nil
}

// collectDeletable recursively walks the config items tree and appends files
// that qualify for deletion (CompressedStatus set, not unfinished).
func collectDeletable(items map[string]*config.ItemNode, absDir string, candidates *[]deleteCandidate) {
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		node := items[name]
		absPath := filepath.Join(absDir, name)

		if node.Items != nil {
			collectDeletable(node.Items, absPath, candidates)
		} else if node.CompressedStatus != nil && !node.CompressedStatus.Unfinished {
			info, err := os.Stat(absPath)
			if err != nil {
				slog.Warn("Could not stat file — skipping",
					"path", absPath, "error", err)
				continue
			}
			*candidates = append(*candidates, deleteCandidate{
				Path: absPath,
				Size: info.Size(),
			})
		}
	}
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%d KB", bytes/kb)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
