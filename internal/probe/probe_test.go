package probe

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:  "typical duration",
			input: `{"format":{"duration":"123.456789"}}`,
			want:  time.Duration(123.456789 * float64(time.Second)),
		},
		{
			name:  "integer duration",
			input: `{"format":{"duration":"60"}}`,
			want:  60 * time.Second,
		},
		{
			name:  "zero duration",
			input: `{"format":{"duration":"0"}}`,
			want:  0,
		},
		{
			name:    "missing duration field",
			input:   `{"format":{}}`,
			wantErr: true,
		},
		{
			name:    "empty format",
			input:   `{}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "non-numeric duration",
			input:   `{"format":{"duration":"N/A"}}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDuration([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseBitrate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:  "stream bitrate available",
			input: `{"streams":[{"bit_rate":"5200000"}],"format":{"size":"1000000","duration":"10.0"}}`,
			want:  5200,
		},
		{
			name:  "stream bitrate rounds to nearest kbps",
			input: `{"streams":[{"bit_rate":"5200500"}],"format":{"size":"1000000","duration":"10.0"}}`,
			want:  5201,
		},
		{
			name:  "stream bitrate rounds down",
			input: `{"streams":[{"bit_rate":"5200499"}],"format":{"size":"1000000","duration":"10.0"}}`,
			want:  5200,
		},
		{
			name:  "stream bitrate N/A falls back to format",
			input: `{"streams":[{"bit_rate":"N/A"}],"format":{"size":"1000000","duration":"10.0"}}`,
			want:  800, // (1000000 * 8) / 10 / 1000 = 800
		},
		{
			name:  "empty stream bitrate falls back to format",
			input: `{"streams":[{"bit_rate":""}],"format":{"size":"1000000","duration":"10.0"}}`,
			want:  800,
		},
		{
			name:  "no streams falls back to format",
			input: `{"streams":[],"format":{"size":"5000000","duration":"20.0"}}`,
			want:  2000, // (5000000 * 8) / 20 / 1000 = 2000
		},
		{
			name:  "fallback rounds to nearest kbps",
			input: `{"streams":[],"format":{"size":"1250000","duration":"10.0"}}`,
			want:  1000, // (1250000 * 8) / 10 / 1000 = 1000
		},
		{
			name:    "no streams and no format size",
			input:   `{"streams":[],"format":{"duration":"10.0"}}`,
			wantErr: true,
		},
		{
			name:    "no streams and no format duration",
			input:   `{"streams":[],"format":{"size":"1000000"}}`,
			wantErr: true,
		},
		{
			name:    "zero duration in fallback",
			input:   `{"streams":[],"format":{"size":"1000000","duration":"0"}}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{broken`,
			wantErr: true,
		},
		{
			name:    "non-numeric stream bitrate",
			input:   `{"streams":[{"bit_rate":"abc"}],"format":{"size":"1000000","duration":"10.0"}}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBitrate([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseBitrateNonNumericFormatSize(t *testing.T) {
	// Covers the "parse format size" error branch (no streams, non-numeric size).
	_, err := parseBitrate([]byte(`{"streams":[],"format":{"size":"abc","duration":"10.0"}}`))
	if err == nil {
		t.Error("expected error for non-numeric format size, got nil")
	}
}

func TestParseBitrateNonNumericFormatDuration(t *testing.T) {
	// Covers the "parse format duration" error branch (no streams, non-numeric duration).
	_, err := parseBitrate([]byte(`{"streams":[],"format":{"size":"1000000","duration":"abc"}}`))
	if err == nil {
		t.Error("expected error for non-numeric format duration, got nil")
	}
}

func TestVideoDurationErrorOnNonexistentFile(t *testing.T) {
	// Exercises the ffprobe exec error path.
	_, err := VideoDuration("/nonexistent_xyz_video_file.mp4")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestVideoStreamBitrateErrorOnNonexistentFile(t *testing.T) {
	// Exercises the ffprobe exec error path.
	_, err := VideoStreamBitrate("/nonexistent_xyz_video_file.mp4")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}
