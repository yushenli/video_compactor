package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/scanner"
)

func newScanCmd() *cobra.Command {
	var outputPath string
	var force bool

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

			cfg, err := scanner.ScanDirectory(dir)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if err := config.SaveConfig(cfg, outputPath); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			count := countFiles(cfg.Items)
			fmt.Printf("Found %d video file(s). Config written to %s\n", count, outputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output YAML file path (default: <directory>/video_compactor.yaml)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
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
