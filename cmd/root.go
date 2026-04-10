package cmd

import (
	"github.com/spf13/cobra"
	"github.com/yushenli/video_compactor/internal/logging"
)

var (
	logLevel   string
	logFile    string
	errLogFile string
	logCleanup func()
)

var rootCmd = &cobra.Command{
	Use:   "video_compactor",
	Short: "Scan directories for video files and compress them with ffmpeg",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cleanup, err := logging.Setup(logging.Options{
			Level:        logLevel,
			LogFile:      logFile,
			ErrorLogFile: errLogFile,
		})
		if err != nil {
			return err
		}
		logCleanup = cleanup
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if logCleanup != nil {
			logCleanup()
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		"log verbosity level: debug, info, warn, error")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "",
		"write info/debug logs to this file instead of stdout")
	rootCmd.PersistentFlags().StringVar(&errLogFile, "error-log-file", "",
		"write warn/error logs to this file instead of stderr")

	rootCmd.AddCommand(newScanCmd())
	rootCmd.AddCommand(newCompressCmd())
	rootCmd.AddCommand(newDeleteCmd())
}

// Execute is the public entry point called by main.
func Execute() error {
	return rootCmd.Execute()
}
