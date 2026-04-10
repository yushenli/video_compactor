package deleter

import (
	"fmt"
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
		fmt.Println("No files eligible for deletion.")
		return nil
	}

	var totalSize int64
	var failures []string

	for _, candidate := range candidates {
		if dryRun {
			fmt.Printf("[dry-run] %s  (%s)\n", candidate.Path, FormatSize(candidate.Size))
			totalSize += candidate.Size
		} else {
			if err := os.Remove(candidate.Path); err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed to delete %s: %v\n", candidate.Path, err)
				failures = append(failures, candidate.Path)
			} else {
				totalSize += candidate.Size
				fmt.Printf("Deleted %s  (%s)\n", candidate.Path, FormatSize(candidate.Size))
			}
		}
	}

	if dryRun {
		fmt.Printf("\nTotal: %d file(s), %s\n", len(candidates), FormatSize(totalSize))
	} else {
		fmt.Printf("\nDeleted %d file(s), freed %s\n", len(candidates)-len(failures), FormatSize(totalSize))
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
				fmt.Fprintf(os.Stderr, "[warning] could not stat %s: %v — skipping\n", absPath, err)
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
