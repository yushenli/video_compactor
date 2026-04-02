// Package filename provides utilities for remapping camera filenames so that
// alphabetical sorting reflects chronological recording order.
//
// The primary export is [RemapFilename], which rewrites filenames from cameras
// whose naming convention breaks sort order (e.g. GoPro chapter-first names).
// Filenames that don't match any rule pass through unchanged.
//
// The remapping rules are maintained in a table-driven [Rules] slice.
// Adding support for a new camera requires only appending one entry.
package filename

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// RemapRule defines a single filename remapping pattern.
type RemapRule struct {
	// Name is a human-readable label for this rule (e.g. "GoPro Hero5+ HEVC").
	Name string

	// Pattern matches the full basename (with extension).
	// It must define named capture groups used by FormatOutput.
	Pattern *regexp.Regexp

	// FormatOutput produces the remapped stem (without extension) from the
	// regex sub-match map. The caller appends the original extension.
	FormatOutput func(match map[string]string) string
}

// chapterToLetter converts a 1-based chapter number string (e.g. "01"→"a", "02"→"b").
// Returns "?" for out-of-range values.
func chapterToLetter(chapterStr string) string {
	n, err := strconv.Atoi(chapterStr)
	if err != nil || n < 1 || n > 26 {
		return "_" + chapterStr
	}
	return string(rune('a' + n - 1))
}

// goProChapterFormatter returns a FormatOutput function for the common GoPro
// pattern: prefix + filenum + chapterLetter.
func goProChapterFormatter(prefix string) func(map[string]string) string {
	return func(m map[string]string) string {
		return prefix + m["filenum"] + chapterToLetter(m["chapter"])
	}
}

// Rules is the ordered table of all filename remapping rules.
// [RemapFilename] tries each rule in order; the first match wins.
// Exported so that other projects can inspect or append custom rules.
var Rules = []RemapRule{
	// GoPro Hero5+ HEVC: GX<cc><nnnn>.<ext> → GX<nnnn><letter>.<ext>
	{
		Name:         "GoPro Hero5+ HEVC",
		Pattern:      regexp.MustCompile(`(?i)^GX(?P<chapter>\d{2})(?P<filenum>\d{4})\.\w+$`),
		FormatOutput: goProChapterFormatter("GX"),
	},
	// GoPro Hero5+ AVC: GH<cc><nnnn>.<ext> → GH<nnnn><letter>.<ext>
	{
		Name:         "GoPro Hero5+ AVC",
		Pattern:      regexp.MustCompile(`(?i)^GH(?P<chapter>\d{2})(?P<filenum>\d{4})\.\w+$`),
		FormatOutput: goProChapterFormatter("GH"),
	},
	// GoPro Looping: GL<cc><nnnn>.<ext> → GL<nnnn><letter>.<ext>
	{
		Name:         "GoPro Looping",
		Pattern:      regexp.MustCompile(`(?i)^GL(?P<chapter>\d{2})(?P<filenum>\d{4})\.\w+$`),
		FormatOutput: goProChapterFormatter("GL"),
	},
	// GoPro MAX 360: GS<cc><nnnn>.<ext> → GS<nnnn><letter>.<ext>
	{
		Name:         "GoPro MAX 360",
		Pattern:      regexp.MustCompile(`(?i)^GS(?P<chapter>\d{2})(?P<filenum>\d{4})\.\w+$`),
		FormatOutput: goProChapterFormatter("GS"),
	},
	// GoPro Legacy first chapter: GOPR<nnnn>.<ext> → GOPR<nnnn>a.<ext>
	{
		Name:    "GoPro Legacy first chapter",
		Pattern: regexp.MustCompile(`(?i)^GOPR(?P<filenum>\d{4})\.\w+$`),
		FormatOutput: func(m map[string]string) string {
			return "GOPR" + m["filenum"] + "a"
		},
	},
	// GoPro Legacy continuation: GP<cc><nnnn>.<ext> → GOPR<nnnn><letter>.<ext>
	{
		Name:         "GoPro Legacy continuation",
		Pattern:      regexp.MustCompile(`(?i)^GP(?P<chapter>\d{2})(?P<filenum>\d{4})\.\w+$`),
		FormatOutput: goProChapterFormatter("GOPR"),
	},
}

// RemapFilename rewrites a camera filename (basename only, no directory) so
// that alphabetical sorting reflects chronological order. Filenames that don't
// match any rule are returned unchanged.
func RemapFilename(basename string) string {
	for _, rule := range Rules {
		match := matchNamed(rule.Pattern, basename)
		if match == nil {
			continue
		}
		ext := filepath.Ext(basename)
		newStem := rule.FormatOutput(match)
		return newStem + ext
	}
	return basename
}

// CompressedOutputPath returns the output path for a given input path.
// It first remaps the basename for sort-order correctness, then inserts
// ".compressed" before the extension.
//
// Example: "/path/GX021603.MP4" → "/path/GX1603b.compressed.MP4"
func CompressedOutputPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)

	remapped := RemapFilename(base)

	ext := filepath.Ext(remapped)
	stem := strings.TrimSuffix(remapped, ext)
	compressed := fmt.Sprintf("%s.compressed%s", stem, ext)

	if dir == "." {
		return compressed
	}
	return filepath.Join(dir, compressed)
}

// matchNamed runs a regex match and returns a map of named capture groups.
// Returns nil if the pattern doesn't match.
func matchNamed(re *regexp.Regexp, s string) map[string]string {
	match := re.FindStringSubmatch(s)
	if match == nil {
		return nil
	}
	result := make(map[string]string)
	for i, name := range re.SubexpNames() {
		if name != "" {
			result[name] = match[i]
		}
	}
	return result
}
