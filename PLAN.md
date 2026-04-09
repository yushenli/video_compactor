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

---

## Filename Remapping for Sort-Order Correctness

### Problem

Action camera filenames (especially GoPro) embed chapter numbers *before* the file number,
breaking alphabetical sort order. When compressed files are placed together, multi-chapter
recordings don't sort next to each other.

**Example:** GoPro Hero5+ HEVC files use `GX<cc><nnnn>.MP4` where `cc` = chapter, `nnnn` = file number.
- `GX011603.MP4` (file 1603, chapter 1) sorts before `GX011604.MP4` (file 1604, chapter 1)
- But `GX021603.MP4` (file 1603, chapter 2) should be right after `GX011603.MP4`.

### Research: Camera Filename Conventions

#### GoPro

**Hero5+ (2016–present, HEVC/H.265 mode)**
- Pattern: `GX<cc><nnnn>.MP4`
  - `GX` = HEVC video prefix
  - `cc` = 2-digit chapter number (01, 02, 03, …)
  - `nnnn` = 4-digit file number (0001–9999)
  - Examples: `GX011603.MP4`, `GX021603.MP4`, `GX031603.MP4`
- Sorting problem: chapter comes before file number, so chapters of different files interleave.

**Hero5+ (H.264/AVC mode)**
- Pattern: `GH<cc><nnnn>.MP4`
  - Same structure as HEVC but with `GH` prefix.
  - Examples: `GH010042.MP4`, `GH020042.MP4`

**Hero5+ (Looping video)**
- Pattern: `GL<cc><nnnn>.MP4`
  - Same structure, `GL` prefix for looping videos.

**Legacy GoPro (Hero1–4)**
- First chapter: `GOPR<nnnn>.MP4`
- Subsequent chapters: `GP<cc><nnnn>.MP4` (cc starts at 02)
  - Examples: `GOPR1603.MP4`, `GP021603.MP4`, `GP031603.MP4`
- Sorting problem: `GOPR` prefix differs from `GP` prefix, and chapter interleaves.

**GoPro MAX (360 camera)**
- Pattern: `GS<cc><nnnn>.360`
  - Same chapter/file structure, `.360` extension.

#### DJI

**Modern DJI Drones & Action Cameras (Mavic 3, Air 3, Mini 4, Osmo Action 4/5, Pocket 3, etc.)**
- Pattern: `DJI_<YYYYMMDDHHMMSS>_<seq>_D.MP4`
  - Timestamp-based naming — sorts correctly by default. No remapping needed.

**Older DJI (Mavic Pro, Phantom 4, etc.)**
- Pattern: `DJI_<nnnn>.MP4`
  - Simple sequential numbering — sorts correctly. No remapping needed.

#### Google Pixel

Pixel phones use the Google Camera app which names videos with timestamps:
- Pattern: `PXL_YYYYMMDD_HHMMSS<suffix>.mp4`
  - `suffix` may include `~2` for burst/duplicate disambiguation, `.TS` for top shot, or be empty.
  - Examples: `PXL_20240615_143022.mp4`, `PXL_20240615_143022~2.mp4`
- Older Pixel models or stock Android camera: `VID_YYYYMMDD_HHMMSS.mp4`
- **Sorts correctly by default. No remapping needed.**

#### Xiaomi

Xiaomi phones (MIUI/HyperOS camera app) use timestamp-based naming:
- Pattern: `VID_YYYYMMDD_HHMMSS.mp4`
  - Examples: `VID_20240615_143022.mp4`
- Some models may use: `MVI_YYYYMMDD_HHMMSS.mp4` or `MIUI_YYYYMMDD_HHMMSS.mp4`
- **Sorts correctly by default. No remapping needed.**

#### Summary

Only GoPro filenames need remapping. DJI, Google Pixel, and Xiaomi files all use timestamp-based or sequential naming that sorts correctly by default.

### Remapping Rules

| Original Pattern   | Remapped Pattern         | Example                          |
| ------------------ | ------------------------ | -------------------------------- |
| `GX<cc><nnnn>.MP4` | `GX<nnnn><letter>.MP4`   | `GX021603.MP4` → `GX1603b.MP4`   |
| `GH<cc><nnnn>.MP4` | `GH<nnnn><letter>.MP4`   | `GH010042.MP4` → `GH0042a.MP4`   |
| `GL<cc><nnnn>.MP4` | `GL<nnnn><letter>.MP4`   | `GL030100.MP4` → `GL0100c.MP4`   |
| `GOPR<nnnn>.MP4`   | `GOPR<nnnn>a.MP4`        | `GOPR1603.MP4` → `GOPR1603a.MP4` |
| `GP<cc><nnnn>.MP4` | `GOPR<nnnn><letter>.MP4` | `GP021603.MP4` → `GOPR1603b.MP4` |
| `GS<cc><nnnn>.360` | `GS<nnnn><letter>.360`   | `GS020050.360` → `GS0050b.360`   |

Chapter → letter mapping: `01`→`a`, `02`→`b`, `03`→`c`, … `26`→`z` (26 chapters max; GoPro creates ~4 GB chapters, so even a 100 GB recording only needs ~25).

Files not matching any GoPro pattern pass through unchanged (DJI, phone videos, etc.).

### File Structure Change

```
internal/
    filename/
        remap.go       — RemapFilename() exported function + table-driven rules
        remap_test.go  — comprehensive tests
```

### Architecture: Table-Driven Remapping Rules

Rules are maintained as a slice of `RemapRule` structs. Adding a new device/pattern requires only one new line in the table.

```go
// RemapRule defines a single filename remapping pattern.
type RemapRule struct {
    Pattern     *regexp.Regexp // regex with named capture groups: "chapter", "filenum"
    OutputPrefix string        // static prefix for the output (e.g. "GX", "GOPR")
    // FormatOutput takes the named captures and returns the remapped basename (no ext).
    // If nil, a default formatter is used: OutputPrefix + filenum + chapterLetter
    FormatOutput func(match map[string]string) string
}

// rules is the ordered table of all remapping rules. First match wins.
var rules = []RemapRule{
    // GoPro Hero5+ HEVC:  GX<cc><nnnn>.MP4 → GX<nnnn><letter>.MP4
    {Pattern: re(`(?i)^(GX)(\d{2})(\d{4})\..+$`), OutputPrefix: "GX"},
    // GoPro Hero5+ AVC:   GH<cc><nnnn>.MP4 → GH<nnnn><letter>.MP4
    {Pattern: re(`(?i)^(GH)(\d{2})(\d{4})\..+$`), OutputPrefix: "GH"},
    // GoPro Looping:      GL<cc><nnnn>.MP4 → GL<nnnn><letter>.MP4
    {Pattern: re(`(?i)^(GL)(\d{2})(\d{4})\..+$`), OutputPrefix: "GL"},
    // GoPro MAX 360:      GS<cc><nnnn>.360 → GS<nnnn><letter>.360
    {Pattern: re(`(?i)^(GS)(\d{2})(\d{4})\..+$`), OutputPrefix: "GS"},
    // GoPro Legacy first: GOPR<nnnn>.MP4   → GOPR<nnnn>a.MP4  (implicit chapter 01)
    {Pattern: re(`(?i)^(GOPR)(\d{4})\..+$`),       OutputPrefix: "GOPR", /* chapter forced to "a" */},
    // GoPro Legacy cont:  GP<cc><nnnn>.MP4 → GOPR<nnnn><letter>.MP4
    {Pattern: re(`(?i)^(GP)(\d{2})(\d{4})\..+$`),  OutputPrefix: "GOPR"},
}
```

Adding support for a new device is a single append to this slice. The default format function handles the common `prefix + filenum + chapterLetter` pattern; only truly unusual formats need a custom `FormatOutput`.

### Implementation Steps

1. **Create `internal/filename/remap.go`** — new package with exported `RemapFilename(basename string) string`. Uses the table-driven `[]RemapRule` slice; iterates rules in order, first match wins. Non-matching filenames returned unchanged. The rule table and `RemapRule` type are also exported so other projects can inspect or extend them.

2. **Create `internal/filename/remap_test.go`** — tests covering all 6 GoPro patterns, non-matching files (DJI, Pixel, Xiaomi, generic), edge cases (case-insensitive prefix, sidecar files `.LRV`/`.THM`), and boundary conditions (chapter 26 → `z`).

3. **Export `CompressedOutputPath`** — refactor the unexported `compressedOutputPath` in `internal/compressor/compressor.go` to an exported `CompressedOutputPath`. Apply `filename.RemapFilename` to the basename before inserting the `.compressed` suffix. Update callers.

4. **Verify** — `go build ./...` and `go test ./...`

### Output Naming After Remapping

The `.compressed` suffix is inserted between stem and extension, *after* remapping:

```
GX021603.MP4  →  remap  →  GX1603b.MP4  →  compressed  →  GX1603b.compressed.MP4
```

### Verification (Remapping)

11. `go test ./internal/filename/...` — all remapping rules covered
12. Dry-run compress with GoPro files — output paths show remapped names
13. Verify that non-GoPro filenames (DJI, phone) pass through unchanged

---

## Preserve Video Metadata

### Problem

The current `BuildFFmpegArgs()` produces commands like:
```
ffmpeg -i input.mp4 -c:v libx265 -crf 28 -c:a copy output.mp4
```

While ffmpeg copies some global metadata by default, it does **not** reliably preserve:

1. **GPS coordinates** — stored in custom MP4/MOV atoms (e.g., Apple's `©xyz` atom or
   `com.apple.quicktime.location.ISO6709`). These proprietary atoms are silently dropped
   during remuxing.
2. **Custom/proprietary metadata** — camera-specific tags, shooting parameters stored as
   user-defined atoms.
3. **File-system timestamps** — the output file gets the current time as its mtime, losing
   the original creation/modification date.

Creation time stored in the standard `creation_time` global metadata key is preserved by
default, but being explicit with `-map_metadata 0` is safer and more portable.

Audio stream metadata is already preserved via `-c:a copy` (stream copy retains all
stream-level tags).

### Changes

#### 1. Add `-map_metadata 0` and `-movflags` to `BuildFFmpegArgs()`

- `-map_metadata 0` — explicitly copies all global and per-stream metadata from input #0.
  This is ffmpeg's default, but being explicit documents intent and guards against future
  ffmpeg default changes.
- `-movflags +use_metadata_tags+faststart` — instructs the MP4 muxer to write user-defined
  metadata tags, preserving custom atoms like GPS coordinates that would otherwise be
  silently dropped. `faststart` is included for better streaming compatibility.

**File**: `internal/compressor/ffmpeg.go` — `BuildFFmpegArgs()`

#### 2. Copy file-system timestamps (mtime) after ffmpeg

After ffmpeg produces the output file, copy the source file's modification time (mtime)
to the output. This keeps the compressed file's date consistent with the original.

Implementation:
- New helper `CopyFileTimestamp(src, dst string) error` in `internal/compressor/`
- Called in `CompressAll()` after `ExecuteFFmpeg()` succeeds
- Skipped when in dry-run/preview mode (since no output file is created)

**Files**: `internal/compressor/ffmpeg.go` (helper), `internal/compressor/compressor.go` (caller)

### Todos

1. Add `-map_metadata 0` and `-movflags +use_metadata_tags+faststart` to `BuildFFmpegArgs()` — `internal/compressor/ffmpeg.go`
2. Update/add tests in `internal/compressor/ffmpeg_test.go` for the new args
3. Implement `CopyFileTimestamp()` and wire it into `CompressAll()` — skip in dry-run mode
4. Add unit tests for `CopyFileTimestamp()`

### Verification (Metadata)

1. `go test ./internal/compressor/...` — all metadata-related tests pass
2. Dry-run shows `-map_metadata 0` and `-movflags +use_metadata_tags+faststart` in preview output
3. After Phase 2 (real ffmpeg execution): verify GPS/creation_time survive round-trip with `ffprobe -show_format output.mp4`

---

## VA-API Hardware Acceleration

### Background

VA-API (Video Acceleration API) is a Linux interface for GPU-accelerated video encoding/decoding. Using it with ffmpeg can dramatically reduce CPU usage and encoding time on systems with compatible Intel, AMD, or other VA-API-capable GPUs.

The VA-API device is typically `/dev/dri/renderD128` but can vary per system.

### How VA-API changes the ffmpeg command

**Software (current):**
```
ffmpeg -i input.mp4 -c:v libx265 -crf 28 -vf scale=-2:1080 ... output.mp4
```

**VA-API (h265):**
```
ffmpeg -vaapi_device /dev/dri/renderD128 -i input.mp4 \
  -vf 'format=nv12|vaapi,hwupload,scale_vaapi=w=-2:h=1080' \
  -c:v hevc_vaapi -qp 28 ... output.mp4
```

Key differences when VA-API is active:

| Aspect          | Software                  | VA-API                                              |
| --------------- | ------------------------- | --------------------------------------------------- |
| Codec (h264)    | `libx264`                 | `h264_vaapi`                                        |
| Codec (h265)    | `libx265`                 | `hevc_vaapi`                                        |
| Quality flag    | `-crf N`                  | `-qp N`                                             |
| Video filter    | `scale=W:H` (optional)    | `format=nv12\|vaapi,hwupload[,scale_vaapi=w=W:h=H]` |
| Lossless        | `-x265-params lossless=1` | Not supported — print a warning and skip            |
| `-vaapi_device` | n/a                       | prepended before `-i`                               |

The `format=nv12|vaapi,hwupload` step converts frames to NV12 pixel format and uploads them to the GPU. The `scale_vaapi` filter then runs on the GPU. Without scaling, the filter chain is just `format=nv12|vaapi,hwupload`.

### Design

VA-API is **opt-in** via a new `--vaapi-device` CLI flag. When the flag is absent (or empty), behaviour is unchanged (software encoding).

#### 1. New `--vaapi-device` CLI flag (`cmd/compress.go`)

```
--vaapi-device <path>   enable VA-API hardware acceleration using this device
                        (e.g. /dev/dri/renderD128); omit to use software encoding
```

#### 2. `CompressOptions` gains a `VAAPIDevice` field (`internal/compressor/compressor.go`)

```go
type CompressOptions struct {
    MaxJobs     int
    DryRun      bool
    VAAPIDevice string  // empty = software encoding
}
```

Passed straight through to `BuildFFmpegArgs`.

#### 3. `BuildFFmpegArgs` signature change (`internal/compressor/ffmpeg.go`)

Add `vaAPIDevice string` parameter (or use an options struct). When non-empty:

- Prepend `-vaapi_device <device>` before `-i`
- Use `h264_vaapi` / `hevc_vaapi` instead of `libx264` / `libx265`
- Replace `-crf N` with `-qp N`
- Build the VA-API video filter chain:
  - No scaling: `-vf format=nv12|vaapi,hwupload`
  - With scaling: `-vf format=nv12|vaapi,hwupload,scale_vaapi=w=W:h=H`
  - The `-2` sentinel works the same way in `scale_vaapi`
- Skip `-x265-params lossless=1`; emit a stderr warning when CRF is 0 and VA-API is active

#### 4. New helper: `buildVAAPIFilterChain` (`internal/compressor/ffmpeg.go`)

Builds the full `-vf` value for VA-API mode:

```go
func buildVAAPIFilterChain(resolution string, srcW, srcH int) (string, error)
```

- No resolution → `"format=nv12|vaapi,hwupload"`
- With resolution → `"format=nv12|vaapi,hwupload,scale_vaapi=w=W:h=H"` (reuses `settings.ParseResolution`)

#### 5. Task table (`internal/compressor/compressor.go`)

Add a **HW** column: shows the VA-API device path when active, or `(sw)` for software.

### Todos

1. Add `VAAPIDevice string` to `CompressOptions`; thread it through `CompressAll` → `BuildFFmpegArgs`
2. Add `--vaapi-device` CLI flag in `cmd/compress.go`
3. Implement VA-API codepath in `BuildFFmpegArgs` — device flag, codec mapping, `-qp`, lossless warning
4. Implement `buildVAAPIFilterChain` helper
5. Update `printTaskTable` / `fprintTaskTable` with HW column
6. Add tests for VA-API args, filter chain, and codec mapping

### Verification (VA-API)

1. `go test ./internal/compressor/...` — all tests pass
2. `--dry-run --vaapi-device /dev/dri/renderD128` shows correct VA-API command in preview
3. Without `--vaapi-device`, dry-run output is unchanged (software encoding)
