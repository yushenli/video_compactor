package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yushenli/video_compactor/internal/compressor"
	"github.com/yushenli/video_compactor/internal/config"
)

func newCompressCmd() *cobra.Command {
	var configPath string
	var maxJobs int
	var codec string
	var dryRun bool
	var vaAPIDevice string
	var ffmpegOutput string

	cmd := &cobra.Command{
		Use:   "compress <directory>",
		Short: "Compress video files according to the YAML config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]

			if configPath == "" {
				configPath = filepath.Join(dir, config.DefaultConfigFilename)
			}

			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// CLI --codec flag overrides YAML defaults, but per-file/dir YAML settings still win
			if codec != "" {
				cfg.Defaults.Codec = codec
			}

			ffOut, ffCleanup, err := openFFmpegOutput(ffmpegOutput)
			if err != nil {
				return err
			}
			defer ffCleanup()

			opts := compressor.CompressOptions{
				MaxJobs:     maxJobs,
				DryRun:      dryRun,
				VAAPIDevice: vaAPIDevice,
				FFmpegOut:   ffOut,
			}
			return compressor.CompressAll(cfg, dir, opts)
		},
	}

	cmd.Flags().StringVarP(&configPath, "file", "f", "", "YAML config file path (default: <directory>/video_compactor.yaml)")
	cmd.Flags().IntVarP(&maxJobs, "jobs", "j", 1, "number of parallel ffmpeg jobs")
	cmd.Flags().StringVar(&codec, "codec", "", "global codec override: h264 or h265")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print ffmpeg commands without executing")
	cmd.Flags().StringVar(&vaAPIDevice, "vaapi-device", "", "enable VA-API hardware acceleration using this device (e.g. /dev/dri/renderD128)")
	cmd.Flags().StringVar(&ffmpegOutput, "ffmpeg-output", "stderr",
		"where to send ffmpeg process output: stdout, stderr, null, or a file path")
	return cmd
}

// openFFmpegOutput converts the --ffmpeg-output flag value into an io.Writer
// and a cleanup function that closes any opened file handle.
func openFFmpegOutput(dest string) (io.Writer, func(), error) {
	switch dest {
	case "stdout":
		return os.Stdout, func() {}, nil
	case "stderr":
		return os.Stderr, func() {}, nil
	case "null", "discard":
		return io.Discard, func() {}, nil
	default:
		f, err := os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("open ffmpeg output file %s: %w", dest, err)
		}
		return f, func() { f.Close() }, nil
	}
}
