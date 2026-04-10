package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/scanner"
)

func newScanCmd() *cobra.Command {
	var outputPath string
	var force bool
	var filterPattern string

	cmd := &cobra.Command{
		Use:   "scan <directory>",
		Short: "Scan a directory for video files and generate a YAML config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]

			if outputPath == "" {
				outputPath = filepath.Join(dir, config.DefaultConfigFilename)
			}

			if !force {
				if _, err := os.Stat(outputPath); err == nil {
					return fmt.Errorf("config file already exists at %s; use --force to overwrite", outputPath)
				}
			}

			cfg, err := scanner.ScanDirectory(dir, filterPattern)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if err := config.SaveConfig(cfg, outputPath); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			count := countFiles(cfg.Items)
			slog.Info("Scan complete", "files", count, "configPath", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output YAML file path (default: <directory>/video_compactor.yaml)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
	cmd.Flags().StringVar(&filterPattern, "filter", "", "only include files whose relative path matches this regex (partial match)")
	return cmd
}

func countFiles(items map[string]*config.ItemNode) int {
	count := 0
	for _, node := range items {
		if node.Items != nil {
			count += countFiles(node.Items)
		} else {
			count++
		}
	}
	return count
}
