package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/deleter"
)

func newDeleteCmd() *cobra.Command {
	var configPath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "delete <directory>",
		Short: "Delete original video files that have been successfully compressed",
		Long: `Delete original video files whose compressed output has been verified.

Only files with a compressed_status block (and without the unfinished flag)
in the YAML config are eligible for deletion.

By default, --dryrun is enabled: the command lists files that would be deleted
without actually removing them. Pass --dryrun=false to perform the deletion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]

			if configPath == "" {
				configPath = filepath.Join(dir, config.DefaultConfigFilename)
			}

			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if dryRun {
				fmt.Println("Running in dry-run mode. No files will be deleted.")
				fmt.Println("Pass --dryrun=false to actually delete files.")
				fmt.Println()
			}

			return deleter.DeleteOriginals(cfg, dir, dryRun)
		},
	}

	cmd.Flags().StringVarP(&configPath, "file", "f", "", "YAML config file path (default: <directory>/video_compactor.yaml)")
	cmd.Flags().BoolVarP(&dryRun, "dryrun", "d", true, "list files without deleting (default: true)")
	return cmd
}
