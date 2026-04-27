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
	var fromConfigPath string
	var force bool
	var update bool
	var filterPattern string

	cmd := &cobra.Command{
		Use:   "scan <directory>",
		Short: "Scan a directory for video files and generate a YAML config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]

			resolvedOutputPath, resolvedFromConfigPath, overwriteAllowed, err := resolveScanPaths(dir, outputPath, fromConfigPath, force, update)
			if err != nil {
				return err
			}

			if !overwriteAllowed {
				if _, err := os.Stat(resolvedOutputPath); err == nil {
					return fmt.Errorf("config file already exists at %s; use --force to overwrite", resolvedOutputPath)
				}
			}

			var existingConfig *config.Config
			if resolvedFromConfigPath != "" {
				existingConfig, err = config.LoadConfig(resolvedFromConfigPath)
				if err != nil {
					return fmt.Errorf("failed to load existing config: %w", err)
				}
			}

			cfg, err := scanner.ScanDirectory(dir, filterPattern)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if existingConfig != nil {
				config.CopyReusableSettings(cfg, existingConfig)
			}

			if err := config.SaveConfig(cfg, resolvedOutputPath); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			count := countFiles(cfg.Items)
			slog.Info("Scan complete", "files", count, "configPath", resolvedOutputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output YAML file path (default: <directory>/video_compactor.yaml)")
	cmd.Flags().StringVar(&fromConfigPath, "from-config", "", "reuse matching settings from an existing YAML config")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
	cmd.Flags().BoolVar(&update, "update", false, "reuse the output file as the input config and overwrite it")
	cmd.Flags().StringVar(&filterPattern, "filter", "", "only include files whose relative path matches this regex (partial match)")
	return cmd
}

func resolveScanPaths(dir, outputPath, fromConfigPath string, force, update bool) (string, string, bool, error) {
	resolvedOutputPath := outputPath
	if resolvedOutputPath == "" {
		resolvedOutputPath = filepath.Join(dir, config.DefaultConfigFilename)
	}

	if update {
		if fromConfigPath != "" {
			return "", "", false, fmt.Errorf("--update cannot be combined with --from-config")
		}
		fromConfigPath = resolvedOutputPath
		force = true
	}

	return resolvedOutputPath, fromConfigPath, force, nil
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
