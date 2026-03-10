package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "video_compactor",
	Short: "Scan directories for video files and compress them with ffmpeg",
}

func init() {
	rootCmd.AddCommand(newScanCmd())
	rootCmd.AddCommand(newCompressCmd())
}

// Execute is the public entry point called by main.
func Execute() error {
	return rootCmd.Execute()
}
