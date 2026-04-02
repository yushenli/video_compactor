package filename

import (
	"testing"
)

func TestRemapFilename_GoProHero5HEVC(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"GX011603.MP4", "GX1603a.MP4"},
		{"GX021603.MP4", "GX1603b.MP4"},
		{"GX031603.MP4", "GX1603c.MP4"},
		{"GX010001.MP4", "GX0001a.MP4"},
		// case insensitive prefix
		{"gx021603.MP4", "GX1603b.MP4"},
		{"Gx021603.mp4", "GX1603b.mp4"},
		// sidecar files
		{"GX011603.LRV", "GX1603a.LRV"},
		{"GX011603.THM", "GX1603a.THM"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RemapFilename(tt.input)
			if got != tt.want {
				t.Errorf("RemapFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemapFilename_GoProHero5AVC(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"GH010042.MP4", "GH0042a.MP4"},
		{"GH020042.MP4", "GH0042b.MP4"},
		{"gh030042.mp4", "GH0042c.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RemapFilename(tt.input)
			if got != tt.want {
				t.Errorf("RemapFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemapFilename_GoProLooping(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"GL010100.MP4", "GL0100a.MP4"},
		{"GL030100.MP4", "GL0100c.MP4"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RemapFilename(tt.input)
			if got != tt.want {
				t.Errorf("RemapFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemapFilename_GoProMAX360(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"GS010050.360", "GS0050a.360"},
		{"GS020050.360", "GS0050b.360"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RemapFilename(tt.input)
			if got != tt.want {
				t.Errorf("RemapFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemapFilename_GoProLegacy(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		// First chapter: GOPR<nnnn> → GOPR<nnnn>a
		{"GOPR1603.MP4", "GOPR1603a.MP4"},
		{"gopr1603.mp4", "GOPR1603a.mp4"},
		// Continuation: GP<cc><nnnn> → GOPR<nnnn><letter>
		{"GP021603.MP4", "GOPR1603b.MP4"},
		{"GP031603.MP4", "GOPR1603c.MP4"},
		{"gp021603.mp4", "GOPR1603b.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RemapFilename(tt.input)
			if got != tt.want {
				t.Errorf("RemapFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemapFilename_ChapterBoundary(t *testing.T) {
	// Chapter 26 → z
	got := RemapFilename("GX260001.MP4")
	if got != "GX0001z.MP4" {
		t.Errorf("chapter 26: got %q, want %q", got, "GX0001z.MP4")
	}

	// Chapter 27 → out of range → "?"
	got = RemapFilename("GX270001.MP4")
	if got != "GX0001_27.MP4" {
		t.Errorf("chapter 27: got %q, want %q", got, "GX0001?.MP4")
	}
}

func TestRemapFilename_NoMatch(t *testing.T) {
	// These should all pass through unchanged
	unchanged := []string{
		"DJI_20240615143022_0001_D.MP4",
		"DJI_0042.MP4",
		"PXL_20240615_143022.mp4",
		"VID_20240615_143022.mp4",
		"movie.mp4",
		"die_hard.mkv",
		"intro.mov",
		"README.md",
	}
	for _, name := range unchanged {
		t.Run(name, func(t *testing.T) {
			got := RemapFilename(name)
			if got != name {
				t.Errorf("RemapFilename(%q) = %q, want unchanged", name, got)
			}
		})
	}
}

func TestRemapFilename_SortOrder(t *testing.T) {
	// Verify that remapped names of the same file number sort together
	inputs := []string{
		"GX011603.MP4", // chapter 1
		"GX021603.MP4", // chapter 2
		"GX011604.MP4", // different file
	}
	remapped := make([]string, len(inputs))
	for i, in := range inputs {
		remapped[i] = RemapFilename(in)
	}
	// GX1603a.MP4 < GX1603b.MP4 < GX1604a.MP4
	if !(remapped[0] < remapped[1] && remapped[1] < remapped[2]) {
		t.Errorf("sort order broken: %v", remapped)
	}
}

func TestCompressedOutputPath(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"video.mp4", "video.compressed.mp4"},
		{"GX021603.MP4", "GX1603b.compressed.MP4"},
		{"/path/to/GX021603.MP4", "/path/to/GX1603b.compressed.MP4"},
		{"/path/to/movie.mkv", "/path/to/movie.compressed.mkv"},
		{"GOPR1603.MP4", "GOPR1603a.compressed.MP4"},
		{"GP021603.MP4", "GOPR1603b.compressed.MP4"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := CompressedOutputPath(tt.input)
			if got != tt.want {
				t.Errorf("CompressedOutputPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
