# video_compactor — Implementation Plan

## Context

Building a new Go CLI tool from scratch in an empty repository (`github.com/yushenli/video_compactor`). The tool has two commands: `scan` (generate YAML config tree from a directory) and `compress` (run ffmpeg on each file using the settings from the YAML). The repository currently contains only `.gitignore`.

## Phase 1 Scope (current)

Implement everything **except** actual ffmpeg execution:
- `scan` command — fully implemented
- `compress` command — resolves all settings and prints the planned ffmpeg command per file; does **not** invoke ffmpeg
- `internal/compressor/ffmpeg.go` — `BuildFFmpegArgs` fully implemented; `ExecuteFFmpeg` prints `[dry-run] ffmpeg <args>` and returns nil
- Parallelism (`-j`) wired up but trivially fast since no real work is done

Phase 2 (later): replace the stub `ExecuteFFmpeg` with real `os/exec` invocation.

---

## File Structure

```
video_compactor/
├── go.mod
├── main.go
├── cmd/
│   ├── root.go       — cobra root command + Execute()
│   ├── scan.go       — "scan" subcommand
│   └── compress.go   — "compress" subcommand
└── internal/
    ├── config/
    │   ├── model.go  — YAML struct types
    │   └── io.go     — LoadConfig(), SaveConfig()
    ├── scanner/
    │   └── scanner.go — ScanDirectory()
    ├── settings/
    │   └── resolve.go — inheritance logic, tags parsing
    └── compressor/
        ├── compressor.go — CompressAll(), task list building, parallelism
        └── ffmpeg.go     — BuildFFmpegArgs(), ExecuteFFmpeg()
```

---

## Key Types

### `internal/config/model.go`

```go
const DefaultConfigFilename = "video_compactor.yaml"

// Settings can appear at any level (defaults, dir, file).
// All fields optional in YAML.
type Settings struct {
    Quality    string `yaml:"quality,omitempty"`    // named preset or raw CRF int
    Resolution string `yaml:"resolution,omitempty"` // "720p","1080p","4k" OR "1920x1080","1280x720"
    Codec      string `yaml:"codec,omitempty"`       // "h264" or "h265"
    Tags       string `yaml:"tags,omitempty"`        // e.g. "normal,1080p" or "22,4k"
    Skip       bool   `yaml:"skip,omitempty"`
}

// ItemNode = file or directory in the tree.
// Directory nodes have Items; file nodes have Items == nil.
type ItemNode struct {
    Settings `yaml:",inline"`
    Items    map[string]*ItemNode `yaml:"items,omitempty"`
}

type Config struct {
    Defaults Settings             `yaml:"defaults"`
    Items    map[string]*ItemNode `yaml:"items"`
}
```

### `internal/settings/resolve.go`

```go
// Quality preset → CRF (libx265)
var qualityPresets = map[string]int{
    "low": 32, "normal": 28, "high": 23, "lossless": 0,
}

// Named resolution shorthand → pixel height (used with scale=-2:<H>)
var resolutionHeights = map[string]int{
    "720p": 720, "1080p": 1080, "1440p": 1440, "4k": 2160, "2160p": 2160,
}

type ResolvedSettings struct {
    CRF        int    // concrete CRF (0–51)
    Resolution string // "" = no scale filter; stored verbatim from YAML
    Codec      string // "h264" or "h265"
    Skip       bool
}

func ParseTags(tags string) (quality, resolution string)
func ResolveForFile(defaults, dirChain []config.Settings, fileNode config.Settings) (ResolvedSettings, error)

// parseResolution resolves a resolution string to (width, height).
// Named shorthand → width=0 (aspect-ratio-preserving), height from table.
// Raw "WxH" or "W*H" format → explicit width and height.
// Examples:
//   "1080p"     → (0, 1080)    → ffmpeg: scale=-2:1080
//   "4k"        → (0, 2160)    → ffmpeg: scale=-2:2160
//   "1920x1080" → (1920, 1080) → ffmpeg: scale=1920:1080
//   "1920*1080" → (1920, 1080) → ffmpeg: scale=1920:1080
func parseResolution(res string) (width, height int, err error)
```

---

## YAML Example

After `scan /media/movies/` (defaults only, no user edits):

```yaml
defaults:
  codec: h265
  quality: normal

items:
  action:
    items:
      die_hard.mp4: {}
      matrix.mkv: {}
  intro.mp4: {}
```

After user edits:

```yaml
defaults:
  codec: h265
  quality: normal

items:
  action:
    quality: high
    resolution: 1080p
    items:
      die_hard.mp4: {}        # inherits dir: h265, CRF 23, scale=-2:1080
      matrix.mkv:
        resolution: 720p      # overrides dir resolution
  intro.mp4:
    quality: "18"             # raw CRF; inherits h265 from defaults
    codec: h264               # file-level codec override
    resolution: 1920x1080     # raw WxH format → scale=1920:1080
```

---

## CLI Interface

```
video_compactor scan <directory> [-o output.yaml] [--force] [--filter <regex>]
video_compactor compress <directory> [-f list.yaml] [-j N] [--codec h264|h265] [--dry-run]
```

`--force` on scan: overwrite existing YAML (default: fail if file exists).
`--filter <regex>` on scan: only include video files whose relative path partially matches the regex (can match directory components, filename, or both). Returns an error immediately if the regex is invalid.
`-j N` on compress: parallel ffmpeg jobs (default 1).
`--codec` on compress: global codec override; per-file/dir YAML settings still win over this.

---

## Settings Inheritance Algorithm

```
base = ResolvedSettings{Codec: "h265"}    // hardcoded fallback
base = applyNodeSettings(base, defaults)  // [defaults] section
for each dir from root to immediate parent:
    base = applyNodeSettings(base, dirSettings)
base = applyNodeSettings(base, fileSettings)
```

Within `applyNodeSettings`, at each level:
1. Tags are parsed and applied first (lower priority at that level)
2. Explicit `Quality`, `Resolution`, `Codec`, `Skip` override tags at that same level
3. `Skip: true` on a directory propagates to all descendant files

---

## ffmpeg Invocation

```
ffmpeg -i <input> -c:v lib<codec> -crf <N> [-vf scale=<W>:<H>] [-x265-params lossless=1] -c:a copy <output>
```

- No `-vf` flag when resolution is empty (keep source resolution)
- `-x265-params lossless=1` only when `CRF==0 && Codec=="h265"`
- Named shorthand (e.g. `1080p`): `-vf scale=-2:1080` (preserves aspect ratio)
- Raw `WxH` / `W*H` (e.g. `1920x1080`): `-vf scale=1920:1080` (exact dimensions)

Output naming: `video.mp4` → `video.compressed.mp4`

---

## Parallelism

Semaphore-based worker pool in `CompressAll`:
```go
sem := make(chan struct{}, opts.MaxJobs)
// goroutine per task, acquire sem before, release after
```

---

## Scan Behavior

- Uses `filepath.WalkDir`
- Included extensions (case-insensitive): `.mp4 .mkv .mov .avi .mpg`
- Skips files whose basename matches `*.compressed.*` (already processed)
- `map[string]*ItemNode` keys are always sorted alphabetically by yaml.v3 during marshal
- When `--filter` is provided, each video file's relative path is tested with `re.MatchString(relPath)` (partial match); only matching files are inserted into the tree. Directory nodes with no matching files are never created.

---

## `--filter` Flag: Regex Filtering on Scan

### Behaviour
- `--filter <pattern>` (string, default empty = no filtering)
- The regex is compiled at the start of `ScanDirectory`; an invalid pattern returns an error before any walking
- Match is **partial** — `regexp.MatchString(pattern, relPath)` — anchoring is the caller's responsibility
- The relative path uses the OS path separator (e.g. `action/die_hard.mp4` on Linux/macOS)

### Implementation changes
1. **`cmd/scan.go`** — add `filterPattern string` flag variable; pass it to `scanner.ScanDirectory`
2. **`internal/scanner/scanner.go`** — change signature to `ScanDirectory(rootDir, filterPattern string)`:
   - If `filterPattern != ""`, compile it; return error on bad pattern
   - In WalkDir callback, skip file if `!re.MatchString(relPath)`
   - `insertFileNode` unchanged — only called for matched files

### Examples

Given this tree under `/media/`:
```
/media/
  action/
    die_hard.mp4
    matrix.mkv
  comedy/
    airplane.mp4
  intro.mp4
```

| `--filter` value | Files included in YAML |
|---|---|
| *(omitted)* | all four files |
| `action` | `action/die_hard.mp4`, `action/matrix.mkv` |
| `die_hard` | `action/die_hard.mp4` only |
| `\.mp4$` | `action/die_hard.mp4`, `comedy/airplane.mp4`, `intro.mp4` |
| `action/die` | `action/die_hard.mp4` only |
| `^intro` | `intro.mp4` only |
| `comedy\|intro` | `comedy/airplane.mp4`, `intro.mp4` |
| `[invalid` | error printed, exit non-zero |

---

## Dependencies

```
github.com/spf13/cobra v1.7.0   — CLI flag/subcommand handling
gopkg.in/yaml.v3 v3.0.0         — YAML marshal/unmarshal
```

No other external dependencies.

---

## Verification

1. `go build ./...` — compiles cleanly
2. `./video_compactor scan /path/to/videos` — creates `video_compactor.yaml`; inspect YAML structure
3. Edit YAML to add quality/resolution overrides
4. `./video_compactor compress /path/to/videos --dry-run` — prints ffmpeg commands without executing
5. Verify argument correctness in dry-run output (CRF values, codecs, scale filters, output paths)
6. `./video_compactor compress /path/to/test/dir -j 2` — runs actual compression; verify `.compressed.` files appear
7. Second `scan` run on same dir — `.compressed.` files should not appear in config
8. `./video_compactor scan /path/to/videos --filter "action"` — YAML contains only files under `action/`
9. `./video_compactor scan /path/to/videos --filter "\.mp4$"` — YAML contains only `.mp4` files
10. `./video_compactor scan /path/to/videos --filter "[invalid"` — prints error and exits non-zero
