# Plan: Orientation-Aware Named Resolution + `-2` Sentinel

## Q1 — Use `-2` instead of `0` as the aspect-ratio sentinel

Currently `ParseResolution` returns `(0, h, nil)` for named shorthands, and `buildScaleFilter` in `ffmpeg.go` has a special `if w == 0` branch. Fix: return `(-2, h, nil)` directly from `ParseResolution` for named resolutions; remove the `if w == 0` branch in `buildScaleFilter`.

## Q2 — Orientation-aware named resolutions (shorter-edge logic)

For named shorthands (`720p`, `1080p`, etc.), the pixel count targets the **shorter** edge:
- Landscape (srcW ≥ srcH): shorter = height → `scale=-2:H`
- Portrait (srcH > srcW): shorter = width → `scale=W:-2`

For raw `WxH`: absolute — no orientation logic, no probe.

## Architecture (keeping `settings` free of I/O)

- `probeVideoDimensions(filePath string) (int, int, error)` lives in `internal/compressor/ffmpeg.go`
- `ParseResolution(res string, srcW, srcH int) (width, height int, err error)` — caller passes in already-probed dims (or 0,0 for fallback)
- Probe is triggered in `BuildFFmpegArgs`, before calling `buildScaleFilter`, **only when** `s.Resolution` is a named shorthand (not raw WxH, not empty)

**Probe trigger logic (in `BuildFFmpegArgs`):**
```go
srcW, srcH := 0, 0
if settings.IsNamedResolution(s.Resolution) {
    var err error
    srcW, srcH, err = probeVideoDimensions(inputPath)  // error → fallback to 0,0
    if err != nil || srcW == 0 || srcH == 0 {
        fmt.Fprintf(os.Stderr, "[warning] unable to probe ...")
    }
}
scaleArg, err := buildScaleFilter(s.Resolution, srcW, srcH)
```

> **Note:** the inner assignment must use `var err error` + `=` (not `:=`) to avoid shadowing the outer `srcW`/`srcH` variables.

**Probe command (JSON format with rotation side_data):**
```
ffprobe -v error -select_streams v:0
        -show_entries stream=width,height
        -show_entries stream_side_data=rotation
        -of json <file>
```
The raw stored `width`/`height` may not reflect display orientation (e.g. DJI/iOS/Android cameras store footage with a rotation tag rather than re-encoding). The probe reads `side_data_list[].rotation` and swaps `w`/`h` when rotation is `±90°` or `±270°` to obtain the true display dimensions.

**Fallback:** probe error or `srcW/srcH == 0` → treat as landscape (`-2, namedH`).

## Changes

### `internal/settings/resolve.go`
- Add exported `IsNamedResolution(res string) bool` — checks `namedResolutionHeights` map (co-located with that map)
- Change `ParseResolution(res string)` → `ParseResolution(res string, srcW, srcH int)`:
  - Named resolution: check `srcH > srcW` → portrait returns `(namedH, -2, nil)`; landscape/unknown returns `(-2, namedH, nil)`
  - Raw WxH: `srcW/srcH` ignored; returns `(w, h, nil)` unchanged

### `internal/compressor/ffmpeg.go`
- Add `probeVideoDimensions(filePath string) (int, int, error)` (unexported): uses `-of json` with `stream_side_data=rotation`; swaps `w`/`h` when `abs(rotation) % 360` is `90` or `270` to get true display dimensions
- `BuildFFmpegArgs`: call `settings.IsNamedResolution(s.Resolution)` to decide whether to probe; use `var err error` + `=` (not `:=`) to avoid variable shadowing; pass `srcW, srcH` to `buildScaleFilter`; emit warning to stderr on probe failure
- `buildScaleFilter(resolution string, srcW, srcH int)` — passes dims into `settings.ParseResolution`; always emits `scale=%d:%d` (removes `if w == 0` branch)
- Add `"os/exec"` and `"encoding/json"` to imports

## Function Ownership Summary

| Function | File | Exported |
|---|---|---|
| `IsNamedResolution(res string) bool` | `internal/settings/resolve.go` | yes |
| `ParseResolution(res string, srcW, srcH int)` | `internal/settings/resolve.go` | yes |
| `probeVideoDimensions(filePath string)` | `internal/compressor/ffmpeg.go` | no |
| `buildScaleFilter(resolution string, srcW, srcH int)` | `internal/compressor/ffmpeg.go` | no |
| `BuildFFmpegArgs(...)` | `internal/compressor/ffmpeg.go` | yes |

## Verification

1. `go build ./...` — compiles cleanly
2. Landscape file (e.g. 1920×1080), `resolution: 1080p` → dry-run shows `scale=-2:1080`
3. Portrait file with rotation metadata (e.g. DJI stored as 2688×1512 with `rotation: -90`), `resolution: 1080p` → probe swaps to effective 1512×2688, dry-run shows `scale=1080:-2`
4. Raw `resolution: 1920x1080` → always `scale=1920:1080` (no probe, no `-2`)
5. Probe fails (bad file / ffprobe not found) → `scale=-2:1080` (safe landscape fallback, no crash)
