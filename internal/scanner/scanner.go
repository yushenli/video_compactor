package scanner

import (
	"fmt"
	"io/fs"
	"log/slog"
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

// walkDirEntryHook, if non-nil, is called for every entry the WalkDir callback
// receives (before any pruning or filtering logic). Tests use this to observe
// which paths were actually visited during the walk.
var walkDirEntryHook func(relPath string, isDir bool)

// ScanDirectory walks rootDir, finds video files (skipping *.compressed.* files),
// and returns a Config whose Items tree mirrors the directory structure.
// If filterPattern is non-empty, only files whose relative path matches the regex are included.
// When the filter contains an anchored prefix (e.g. ^2026 or ^202[456]), directories
// that cannot contain matching files are skipped entirely for performance.
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

	// Pre-compile prefix regex and ancestor regexes for directory pruning.
	prefixPattern := extractPrefixPattern(filterPattern)
	var prefixRegex *regexp.Regexp
	var ancestorRegexes []*regexp.Regexp
	if prefixPattern != "" {
		prefixRegex = regexp.MustCompile("^" + prefixPattern)
		for _, seg := range splitPrefixAtSlashes(prefixPattern) {
			ancestorRegexes = append(ancestorRegexes, regexp.MustCompile("^"+seg+"/$"))
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
		if walkDirEntryHook != nil {
			relEntry, relErr := filepath.Rel(rootDir, path)
			if relErr == nil {
				walkDirEntryHook(filepath.ToSlash(relEntry), d.IsDir())
			}
		}
		if d.IsDir() {
			if prefixRegex == nil {
				return nil
			}
			relDir, relErr := filepath.Rel(rootDir, path)
			if relErr != nil || relDir == "." {
				return nil
			}
			relDir = filepath.ToSlash(relDir)
			dirSlash := relDir + "/"
			// Condition A: directory is at or deeper than the prefix target.
			if prefixRegex.MatchString(dirSlash) {
				return nil
			}
			// Condition B: directory is an ancestor on the right path.
			for _, ar := range ancestorRegexes {
				if ar.MatchString(dirSlash) {
					return nil
				}
			}
			slog.Debug("Skipping directory (prefix prune)", "dir", relDir)
			return filepath.SkipDir
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

// extractPrefixPattern extracts the longest regex sub-pattern from the beginning
// of pattern that every match must start with. Returns "" if no useful prefix
// can be extracted (e.g., pattern is not ^-anchored).
// The returned string is a valid regex fragment that may include character classes
// like [456] and backslash-escaped punctuation like \. or \\.
func extractPrefixPattern(pattern string) string {
	if len(pattern) == 0 || pattern[0] != '^' {
		return ""
	}

	// tokens tracks the pattern fragment contributed by each parsed token.
	// When a quantifier is found, the last token is dropped.
	var tokens []string
	i := 1
	for i < len(pattern) {
		ch := pattern[i]
		switch {
		case ch == '[':
			// Scan the character class to the matching ].
			token, end := scanCharClass(pattern, i)
			if end < 0 {
				// Unterminated class — stop.
				goto done
			}
			tokens = append(tokens, token)
			i = end
		case ch == '\\':
			if i+1 >= len(pattern) {
				goto done
			}
			next := pattern[i+1]
			if isRegexShorthand(next) {
				goto done
			}
			// Punctuation escape — keep the escaped form.
			tokens = append(tokens, pattern[i:i+2])
			i += 2
		case ch == '*' || ch == '+' || ch == '?' || ch == '{':
			// Quantifier: drop the last token.
			if len(tokens) > 0 {
				tokens = tokens[:len(tokens)-1]
			}
			goto done
		case ch == '(' || ch == '.' || ch == '|' || ch == '$' || ch == '^':
			goto done
		default:
			tokens = append(tokens, string(ch))
			i++
		}
	}
done:
	if len(tokens) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t)
	}
	return b.String()
}

// scanCharClass scans a character class starting at pattern[start] == '['.
// It returns the full class string (e.g., "[456]") and the index just past
// the closing ']'. Returns ("", -1) if no closing ']' is found.
func scanCharClass(pattern string, start int) (string, int) {
	i := start + 1
	// Handle negation and leading ']' (which is literal in this position).
	if i < len(pattern) && pattern[i] == '^' {
		i++
	}
	if i < len(pattern) && pattern[i] == ']' {
		i++
	}
	for i < len(pattern) {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			i += 2
			continue
		}
		if pattern[i] == ']' {
			return pattern[start : i+1], i + 1
		}
		i++
	}
	return "", -1
}

// isRegexShorthand returns true if ch (the character after '\') represents
// a regex shorthand class or assertion rather than a literal escape.
func isRegexShorthand(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// splitPrefixAtSlashes splits a prefix pattern at unescaped '/' boundaries
// (respecting [...] groups and \-escapes) and returns the sub-patterns for
// building ancestor regexes.
// For "201[456]/Jan", it returns ["201[456]"].
// For "a[bc]/d[ef]/ghi", it returns ["a[bc]", "a[bc]/d[ef]"].
func splitPrefixAtSlashes(prefixPattern string) []string {
	var segments []string
	i := 0
	for i < len(prefixPattern) {
		ch := prefixPattern[i]
		switch {
		case ch == '[':
			_, end := scanCharClass(prefixPattern, i)
			if end < 0 {
				i++
			} else {
				i = end
			}
		case ch == '\\' && i+1 < len(prefixPattern):
			i += 2
		case ch == '/':
			segments = append(segments, prefixPattern[:i])
			i++
		default:
			i++
		}
	}
	return segments
}

// probeCompressedStatus checks whether a compressed target file exists for the
// given original file and returns a CompressedStatus describing the result.
// Returns nil when no compressed target exists.
func probeCompressedStatus(originalPath string) *config.CompressedStatus {
	targetPath := filename.CompressedOutputPath(originalPath)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return nil
	}
	slog.Info("Probing potentially compressed video", "originalPath", originalPath, "targetPath", targetPath)

	origDuration, err := probeDuration(originalPath)
	if err != nil {
		slog.Warn("Could not probe duration — marking as unfinished", "file", originalPath, "error", err)
		return &config.CompressedStatus{Unfinished: true}
	}
	targetDuration, err := probeDuration(targetPath)
	if err != nil {
		slog.Warn("Could not probe duration — marking as unfinished", "file", targetPath, "error", err)
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
		slog.Warn("Could not stat file — marking as unfinished", "file", originalPath, "error", err)
		return &config.CompressedStatus{Unfinished: true}
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		slog.Warn("Could not stat file — marking as unfinished", "file", targetPath, "error", err)
		return &config.CompressedStatus{Unfinished: true}
	}

	origBitrate, err := probeBitrate(originalPath)
	if err != nil {
		slog.Warn("Could not probe bitrate — marking as unfinished", "file", originalPath, "error", err)
		return &config.CompressedStatus{Unfinished: true}
	}
	targetBitrate, err := probeBitrate(targetPath)
	if err != nil {
		slog.Warn("Could not probe bitrate — marking as unfinished", "file", targetPath, "error", err)
		return &config.CompressedStatus{Unfinished: true}
	}

	ratio := 100.0
	if origInfo.Size() > 0 {
		ratio = float64(targetInfo.Size()) / float64(origInfo.Size()) * 100
	}
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
