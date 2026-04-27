package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
)

func TestNewScanCmdRegistersFlags(t *testing.T) {
	cmd := newScanCmd()

	tests := []struct {
		flagName    string
		wantDefault string
	}{
		{"output", ""},
		{"from-config", ""},
		{"force", "false"},
		{"update", "false"},
		{"filter", ""},
	}

	for _, tc := range tests {
		t.Run(tc.flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tc.flagName)
			if flag == nil {
				t.Fatalf("expected --%s flag to be registered", tc.flagName)
			}
			if flag.DefValue != tc.wantDefault {
				t.Errorf("--%s default = %q, want %q", tc.flagName, flag.DefValue, tc.wantDefault)
			}
		})
	}
}

func TestResolveScanPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dir            string
		outputPath     string
		fromConfigPath string
		force          bool
		update         bool
		wantOutputPath string
		wantFromPath   string
		wantForce      bool
		wantErr        string
	}{
		{
			name:           "default output path",
			dir:            "/videos",
			wantOutputPath: filepath.Join("/videos", config.DefaultConfigFilename),
		},
		{
			name:           "explicit output path preserved",
			dir:            "/videos",
			outputPath:     "/tmp/custom.yaml",
			wantOutputPath: "/tmp/custom.yaml",
		},
		{
			name:           "from config preserved",
			dir:            "/videos",
			outputPath:     "/tmp/custom.yaml",
			fromConfigPath: "/tmp/old.yaml",
			wantOutputPath: "/tmp/custom.yaml",
			wantFromPath:   "/tmp/old.yaml",
		},
		{
			name:           "update reuses output and forces overwrite",
			dir:            "/videos",
			outputPath:     "/tmp/custom.yaml",
			update:         true,
			wantOutputPath: "/tmp/custom.yaml",
			wantFromPath:   "/tmp/custom.yaml",
			wantForce:      true,
		},
		{
			name:           "update uses default output path",
			dir:            "/videos",
			update:         true,
			wantOutputPath: filepath.Join("/videos", config.DefaultConfigFilename),
			wantFromPath:   filepath.Join("/videos", config.DefaultConfigFilename),
			wantForce:      true,
		},
		{
			name:           "update cannot combine with from config",
			dir:            "/videos",
			outputPath:     "/tmp/custom.yaml",
			fromConfigPath: "/tmp/old.yaml",
			update:         true,
			wantErr:        "--update cannot be combined with --from-config",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOutputPath, gotFromPath, gotForce, err := resolveScanPaths(tc.dir, tc.outputPath, tc.fromConfigPath, tc.force, tc.update)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("error = %q, want %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotOutputPath != tc.wantOutputPath {
				t.Errorf("output path = %q, want %q", gotOutputPath, tc.wantOutputPath)
			}
			if gotFromPath != tc.wantFromPath {
				t.Errorf("from-config path = %q, want %q", gotFromPath, tc.wantFromPath)
			}
			if gotForce != tc.wantForce {
				t.Errorf("force = %v, want %v", gotForce, tc.wantForce)
			}
		})
	}
}

func TestCountFiles(t *testing.T) {
	tests := []struct {
		name  string
		items map[string]*config.ItemNode
		want  int
	}{
		{
			name:  "empty",
			items: map[string]*config.ItemNode{},
			want:  0,
		},
		{
			name: "flat_files",
			items: map[string]*config.ItemNode{
				"a.mp4": {},
				"b.mp4": {},
			},
			want: 2,
		},
		{
			name: "nested_directory",
			items: map[string]*config.ItemNode{
				"dir": {
					Items: map[string]*config.ItemNode{
						"c.mp4": {},
						"d.mp4": {},
					},
				},
				"e.mp4": {},
			},
			want: 3,
		},
		{
			name: "deeply_nested",
			items: map[string]*config.ItemNode{
				"a": {
					Items: map[string]*config.ItemNode{
						"b": {
							Items: map[string]*config.ItemNode{
								"deep.mp4": {},
							},
						},
					},
				},
			},
			want: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countFiles(tc.items)
			if got != tc.want {
				t.Errorf("countFiles() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestScanCommandReusesMatchingSettingsFromConfig(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "shows"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
		t.Fatalf("WriteFile(movie): %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "shows", "episode.mp4"), nil, 0644); err != nil {
		t.Fatalf("WriteFile(episode): %v", err)
	}

	fromConfigPath := filepath.Join(t.TempDir(), "old.yaml")
	oldConfig := &config.Config{
		Defaults: config.Settings{Quality: "normal", Codec: "h265"},
		Items: map[string]*config.ItemNode{
			"movie.mp4": {
				Settings: config.Settings{
					Quality:    "high",
					Resolution: "1080p",
					Codec:      "h264",
					Tags:       "favorite",
					Skip:       true,
				},
				CompressedStatus: &config.CompressedStatus{CompressedRatio: "99%"},
			},
			"shows": {
				Settings: config.Settings{
					Quality: "low",
					Tags:    "tv",
				},
				Items: map[string]*config.ItemNode{
					"episode.mp4": {
						Settings: config.Settings{Resolution: "720p"},
					},
				},
			},
		},
	}
	if err := config.SaveConfig(oldConfig, fromConfigPath); err != nil {
		t.Fatalf("SaveConfig(old): %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "new.yaml")
	cmd := newScanCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{rootDir, "--output", outputPath, "--from-config", fromConfigPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	newConfig, err := config.LoadConfig(outputPath)
	if err != nil {
		t.Fatalf("LoadConfig(new): %v", err)
	}

	movie := newConfig.Items["movie.mp4"]
	if movie == nil {
		t.Fatal("movie.mp4 should exist")
	}
	if movie.Quality != "high" || movie.Resolution != "1080p" || movie.Codec != "h264" || movie.Tags != "favorite" || !movie.Skip {
		t.Fatalf("movie.mp4 settings not copied correctly: %+v", movie.Settings)
	}
	if movie.CompressedStatus != nil {
		t.Fatalf("movie.mp4 compressed status should come from the new scan, not the old config, got %+v", movie.CompressedStatus)
	}

	shows := newConfig.Items["shows"]
	if shows == nil {
		t.Fatal("shows directory should exist")
	}
	if shows.Quality != "low" || shows.Tags != "tv" {
		t.Fatalf("shows directory settings not copied correctly: %+v", shows.Settings)
	}
	if shows.CompressedStatus != nil {
		t.Fatalf("directory nodes should not gain compressed status, got %+v", shows.CompressedStatus)
	}

	episode := shows.Items["episode.mp4"]
	if episode == nil {
		t.Fatal("episode.mp4 should exist")
	}
	if episode.Resolution != "720p" {
		t.Fatalf("episode.mp4 resolution not copied correctly: %+v", episode.Settings)
	}
}

func TestScanCommandUpdateReusesOutputFile(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
		t.Fatalf("WriteFile(movie): %v", err)
	}

	outputPath := filepath.Join(rootDir, config.DefaultConfigFilename)
	oldConfig := &config.Config{
		Defaults: config.Settings{Quality: "normal", Codec: "h265"},
		Items: map[string]*config.ItemNode{
			"movie.mp4": {
				Settings: config.Settings{
					Quality:    "high",
					Resolution: "1080p",
					Codec:      "h264",
					Tags:       "favorite",
					Skip:       true,
				},
			},
		},
	}
	if err := config.SaveConfig(oldConfig, outputPath); err != nil {
		t.Fatalf("SaveConfig(old): %v", err)
	}

	cmd := newScanCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{rootDir, "--update"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	newConfig, err := config.LoadConfig(outputPath)
	if err != nil {
		t.Fatalf("LoadConfig(new): %v", err)
	}

	movie := newConfig.Items["movie.mp4"]
	if movie == nil {
		t.Fatal("movie.mp4 should exist")
	}
	if movie.Quality != "high" || movie.Resolution != "1080p" || movie.Codec != "h264" || movie.Tags != "favorite" || !movie.Skip {
		t.Fatalf("movie.mp4 settings not copied correctly during --update: %+v", movie.Settings)
	}
}

func TestScanCommandErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) []string
		wantErr string
	}{
		{
			name: "existing output without force",
			setup: func(t *testing.T) []string {
				rootDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
					t.Fatalf("WriteFile(movie): %v", err)
				}

				outputPath := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(outputPath, []byte("items: {}\n"), 0644); err != nil {
					t.Fatalf("WriteFile(output): %v", err)
				}
				return []string{rootDir, "--output", outputPath}
			},
			wantErr: "config file already exists",
		},
		{
			name: "invalid existing config",
			setup: func(t *testing.T) []string {
				rootDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
					t.Fatalf("WriteFile(movie): %v", err)
				}

				fromConfigPath := filepath.Join(t.TempDir(), "bad.yaml")
				if err := os.WriteFile(fromConfigPath, []byte(":\tinvalid:\t[\n"), 0644); err != nil {
					t.Fatalf("WriteFile(from-config): %v", err)
				}

				outputPath := filepath.Join(t.TempDir(), "config.yaml")
				return []string{rootDir, "--output", outputPath, "--from-config", fromConfigPath}
			},
			wantErr: "failed to load existing config",
		},
		{
			name: "invalid filter bubbles as scan failure",
			setup: func(t *testing.T) []string {
				rootDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
					t.Fatalf("WriteFile(movie): %v", err)
				}

				outputPath := filepath.Join(t.TempDir(), "config.yaml")
				return []string{rootDir, "--output", outputPath, "--filter", "[invalid"}
			},
			wantErr: "scan failed: invalid --filter regex",
		},
		{
			name: "save failure bubbles as write error",
			setup: func(t *testing.T) []string {
				rootDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
					t.Fatalf("WriteFile(movie): %v", err)
				}

				outputDir := filepath.Join(t.TempDir(), "config-dir")
				if err := os.MkdirAll(outputDir, 0755); err != nil {
					t.Fatalf("MkdirAll(outputDir): %v", err)
				}
				return []string{rootDir, "--output", outputDir, "--force"}
			},
			wantErr: "failed to write config",
		},
		{
			name: "update cannot combine with from config",
			setup: func(t *testing.T) []string {
				rootDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(rootDir, "movie.mp4"), nil, 0644); err != nil {
					t.Fatalf("WriteFile(movie): %v", err)
				}

				fromConfigPath := filepath.Join(t.TempDir(), "old.yaml")
				if err := os.WriteFile(fromConfigPath, []byte("items: {}\n"), 0644); err != nil {
					t.Fatalf("WriteFile(from-config): %v", err)
				}

				outputPath := filepath.Join(t.TempDir(), "config.yaml")
				return []string{rootDir, "--output", outputPath, "--from-config", fromConfigPath, "--update"}
			},
			wantErr: "--update cannot be combined with --from-config",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newScanCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.setup(t))

			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected command to fail, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}
