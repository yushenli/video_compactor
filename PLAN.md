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

## Cleanup already compressed files

### Background

 After videos files listed in the YAML config file have been compressed, the user may choose to run
 the tool again to remove the original files in order to save disk space.

### How cleanup works

The cleanup process involves two steps. The first step involves calling the scan command one more time,
which will find out which original files have been already compressed. The second step involves running
a new sub-command called "delete", which will actually removed the original files.

#### The "scan" step

When the tool walks through a directory in the scan sub-command, for each video file it sees, it should
check if the target file already exists. If it does, the tool should do the following:
1. Check if the previous compression work completed. This should be done by checking if the target
   video's length is within a 2-second difference from the original video. This step is necessary
   as sometimes compress works are interrupted and leaves a half done output file.
2. If the target video is considered completed, find out the *size* an d *average video stream bitrate*
   of *both* the original file and the target file.
3. In the output YAML file, for each matched video file, add a new block called `compressed_status`. For
   target videos not within a 2-second difference from the original video, add a field called `unfinished`
   and set its value to true. For other matched videos, add the following fields:
   1. compressed_ratio: the size of the target file divided by the original file, expressed in a
      percentage, rounded to the nearest full number.
   2. bitrate_origin: The bitrate of the original video, in kbps.
   3. bitrate_target: The bitrate of the target video, in kbps.
   Notes:
   1. *Do not* generate the `compressed_status` field for original videos not matched to a compressed one.
   2. *Do not* generate the `unfinished` field for the completed ones.
4. The YAML file will be generated the same way as a normal scan. The user may choose the examine the
   output. The user may choose to delete the `compressed_status` field from a video, and if they do so,
   that file will just work as a regular file in the YAML. In another words, if the user runs the `compress`
   command afterwards, that video file will be compressed again.

#### The "delete" step

A new sub-command called "delete" should be created. It will also take a directory name and the YAML
filename as the input. It then looks at all the video files with a `compressed_status` and without the
`unfinished` field and delete the file permanently.

The sub-command should come with a --dry-run flag, which is **by default set to true**. In dry run mode
it will print out the full filenames of all the files to be deleted.

The tool should also output the size of each file and total size to be deleted, in human readable
format (i.e. using the closest GB or MB). This should be done both in dryrun and non-dryrun mode.

### Implementation Design

#### Clarifications from design review

- **Target file discovery**: Use the existing `filename.CompressedOutputPath()` function to derive the
  expected compressed file path from the original.
- **Bitrate fields**: `bitrate_origin` and `bitrate_target` refer to the **video stream bitrate only**
  (not overall file bitrate including audio).
- **`unfinished` field value**: Should be `true` (not `false`) when the target is incomplete.
- **Delete target**: The delete command only deletes the **original** file (keeps the compressed one).
- **Delete error handling**: If a file fails to delete (e.g., permission denied), continue deleting
  other files, collect errors, print a summary of failures at the end, and exit with non-zero status.
- **ffprobe failure during scan**: If ffprobe fails on either the original or compressed file, log a
  warning to stderr and mark the file as `unfinished: true` so the user can manually inspect.

#### Changes to existing files

##### `internal/config/model.go` — Add CompressedStatus struct

Add a new struct and a pointer field on `ItemNode`:

```go
// CompressedStatus holds metadata about a previously compressed output file.
// Present only when the scanner detects a matching .compressed.* file.
type CompressedStatus struct {
    Unfinished      bool   `yaml:"unfinished,omitempty"`
    CompressedRatio string `yaml:"compressed_ratio,omitempty"` // e.g. "42%"
    BitrateOrigin   int    `yaml:"bitrate_origin,omitempty"`   // kbps, rounded
    BitrateTarget   int    `yaml:"bitrate_target,omitempty"`   // kbps, rounded
}

type ItemNode struct {
    Settings         `yaml:",inline"`
    CompressedStatus *CompressedStatus        `yaml:"compressed_status,omitempty"`
    Items            map[string]*ItemNode     `yaml:"items,omitempty"`
}
```

Using a pointer (`*CompressedStatus`) so that:
- `nil` → field omitted from YAML (no compressed file found)
- `&CompressedStatus{Unfinished: true}` → `compressed_status: { unfinished: true }`
- `&CompressedStatus{CompressedRatio: "42%", ...}` → full stats (Unfinished is false/zero-value → omitted)

##### `internal/scanner/scanner.go` — Probe compressed status during scan

Modify the `ScanDirectory` / `insertFileNode` workflow:

1. After determining a file is a video and not a `.compressed.*` file, compute the target path via
   `filename.CompressedOutputPath(absPath)`.
2. `os.Stat()` the target path. If it doesn't exist, proceed as before (no `CompressedStatus`).
3. If the target exists, call a new `probeCompressedStatus(originalPath, targetPath)` function that:
   - Probes duration of both files using ffprobe.
   - If ffprobe fails on either file, logs a warning and returns `&CompressedStatus{Unfinished: true}`.
   - If durations differ by more than 2 seconds, returns `&CompressedStatus{Unfinished: true}`.
   - Otherwise, probes file size (`os.Stat`) and video stream bitrate (rounded to nearest kbps)
     for both files, computes `compressed_ratio` (as a string like `"42%"`), and returns the full
     `CompressedStatus`.
4. Attach the `CompressedStatus` to the `ItemNode`.

The `insertFileNode` function signature changes to accept an optional `*CompressedStatus`:

```go
func insertFileNode(items map[string]*config.ItemNode, relPath string, cs *config.CompressedStatus)
```

##### `cmd/root.go` — Register the delete command

```go
func init() {
    rootCmd.AddCommand(newScanCmd())
    rootCmd.AddCommand(newCompressCmd())
    rootCmd.AddCommand(newDeleteCmd())
}
```

##### `internal/compressor/compressor.go` — Skip already-compressed files

In the `walkItems` function, after checking `resolved.Skip`, add a check for completed compressed
status. A file with `CompressedStatus != nil && !CompressedStatus.Unfinished` should be skipped
(it was already successfully compressed and hasn't been removed from the config by the user):

```go
// File node: resolve settings and add task if not skipped
resolved, err := settings.ResolveForFile(defaults, settingsStack, node.Settings)
if err != nil {
    return fmt.Errorf("%s: %w", absPath, err)
}
if resolved.Skip {
    continue
}
// Skip files already successfully compressed
if node.CompressedStatus != nil && !node.CompressedStatus.Unfinished {
    continue
}
```

This is consistent with the scan design: if the user removes the `compressed_status` field from
a video entry, it will be treated as a fresh file and compressed again.

#### New files

##### `internal/probe/probe.go` — ffprobe helpers for duration, size, bitrate

New package `internal/probe` with exported functions:

```go
package probe

// VideoDuration returns the duration of a video file in seconds.
// Uses: ffprobe -v error -show_entries format=duration -of json <file>
func VideoDuration(filePath string) (float64, error)

// VideoStreamBitrate returns the average bitrate of the first video stream in kbps,
// rounded to the nearest integer.
// Uses: ffprobe -v error -select_streams v:0 -show_entries stream=bit_rate -of json <file>
// If the stream bit_rate is "N/A" (common for some codecs), falls back to computing
// it from format size and duration.
func VideoStreamBitrate(filePath string) (int, error)
```

File size is obtained via `os.Stat()` directly in the scanner — no need for a probe helper.

##### `internal/probe/probe_test.go` — Unit tests

- Test `parseVideoDuration` (internal JSON parser) with sample ffprobe outputs.
- Test `parseVideoStreamBitrate` with sample outputs including "N/A" fallback.
- Integration tests can be skipped in CI if ffprobe isn't available.

##### `cmd/delete.go` — Delete subcommand

```go
func newDeleteCmd() *cobra.Command {
    var configPath string
    var dryRun bool  // default: true

    cmd := &cobra.Command{
        Use:   "delete <directory>",
        Short: "Delete original video files that have been successfully compressed",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            dir := args[0]
            cfg, err := config.LoadConfig(configPath)
            // ... walk tree, collect deletable files, execute
            return deleter.DeleteOriginals(cfg, dir, dryRun)
        },
    }
    cmd.Flags().StringVarP(&configPath, "file", "f", "", "YAML config file")
    cmd.Flags().BoolVarP(&dryRun, "dryrun", "d", true, "list files without deleting (default: true)")
    return cmd
}
```

##### `internal/deleter/deleter.go` — Deletion logic

```go
package deleter

// DeleteOriginals walks the config tree and deletes original video files
// that have a CompressedStatus without the Unfinished flag.
func DeleteOriginals(cfg *config.Config, rootDir string, dryRun bool) error
```

The function:
1. Recursively walks `cfg.Items`, building a sorted list of files to delete.
2. A file qualifies for deletion when:
   - `node.CompressedStatus != nil`
   - `node.CompressedStatus.Unfinished == false`
3. Resolves the full path using `rootDir + relative path from tree`.
4. Gets file size via `os.Stat()`.
5. Prints each file with its size in human-readable format (e.g., `1.2 GB`, `345 MB`).
6. If `!dryRun`, calls `os.Remove()`. On failure, records the error and continues.
7. Prints total size at the end.
8. If any deletions failed, prints failure summary and returns a non-nil error.

##### `internal/deleter/deleter_test.go` — Unit tests

- Test tree walking and file qualification logic.
- Test human-readable size formatting.
- Test dry-run vs actual deletion using temp directories.
- Test error collection when files fail to delete.

#### Human-readable size formatting

Utility function (in `internal/deleter/` or a shared `internal/util/` package):

```go
// FormatSize returns a human-readable size string.
// Uses the nearest unit: bytes, KB, MB, or GB.
func FormatSize(bytes int64) string
```

Rules:
- < 1 KB → "N bytes"
- < 1 MB → "N KB"
- < 1 GB → "N.N MB"
- ≥ 1 GB → "N.N GB"

#### Example YAML output (after scan with compressed status)

```yaml
defaults:
  quality: normal
  codec: h265
items:
  vacation.mp4:
    compressed_status:
      compressed_ratio: "42%"
      bitrate_origin: 5200
      bitrate_target: 2184
  broken_recording.mp4:
    compressed_status:
      unfinished: true
  not_yet_compressed.mp4: {}
```

#### Task breakdown

1. **Add CompressedStatus to config model** — modify `internal/config/model.go`
2. **Create probe package** — new `internal/probe/probe.go` with duration & bitrate helpers
3. **Create probe tests** — new `internal/probe/probe_test.go`
4. **Enhance scanner** — modify `internal/scanner/scanner.go` to detect and probe compressed files
5. **Add scanner tests for compressed status** — update `internal/scanner/scanner_test.go`
6. **Skip compressed in compress command** — modify `internal/compressor/compressor.go` `walkItems` to skip files with completed `CompressedStatus`
7. **Create deleter package** — new `internal/deleter/deleter.go` with deletion logic & size formatting
8. **Create deleter tests** — new `internal/deleter/deleter_test.go`
9. **Add delete CLI command** — new `cmd/delete.go` with `--dryrun` / `-d` flag (default true)
10. **Register delete command** — update `cmd/root.go`
11. **Config roundtrip test** — update `internal/config/io_test.go` for CompressedStatus serialization
