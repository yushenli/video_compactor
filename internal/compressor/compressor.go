package compressor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"github.com/yushenli/video_compactor/internal/config"
	"github.com/yushenli/video_compactor/internal/filename"
	"github.com/yushenli/video_compactor/internal/settings"
)

// CompressTask represents one ffmpeg job.
type CompressTask struct {
	InputPath  string
	OutputPath string
	Settings   settings.ResolvedSettings
}

// CompressOptions carries the runtime options for the compress command.
type CompressOptions struct {
	MaxJobs     int
	DryRun      bool
	VAAPIDevice string // empty = software encoding; non-empty = VA-API device path (e.g. /dev/dri/renderD128)
}

// CompressAll builds the task list from cfg and executes them with opts.MaxJobs parallelism.
func CompressAll(cfg *config.Config, rootDir string, opts CompressOptions) error {
	tasks, err := buildTaskList(cfg, rootDir)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No files to compress.")
		return nil
	}

	printTaskTable(tasks, opts.VAAPIDevice)

	maxJobs := opts.MaxJobs
	if maxJobs < 1 {
		maxJobs = 1
	}

	total := len(tasks)
	var completed int64

	sem := make(chan struct{}, maxJobs)
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(t CompressTask) {
			defer wg.Done()
			defer func() { <-sem }()
			args := BuildFFmpegArgs(t.InputPath, t.OutputPath, t.Settings, opts.VAAPIDevice)
			if err := ExecuteFFmpeg(args, opts.DryRun); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if !opts.DryRun {
				if tsErr := CopyFileTimestamp(t.InputPath, t.OutputPath); tsErr != nil {
					fmt.Fprintf(os.Stderr, "[warning] could not copy timestamp for %s: %v\n", t.OutputPath, tsErr)
				}
			}
			n := atomic.AddInt64(&completed, 1)
			fmt.Printf("[progress] %d out of %d videos compressed\n", n, total)
		}(task)
	}

	wg.Wait()
	return firstErr
}

// printTaskTable prints a formatted table of all compression tasks.
func printTaskTable(tasks []CompressTask, vaAPIDevice string) {
	fprintTaskTable(os.Stdout, tasks, vaAPIDevice)
}

// fprintTaskTable writes the task table to dest, using only the base filename for output paths.
// vaAPIDevice is printed once as a header line; it is not repeated per row.
func fprintTaskTable(dest io.Writer, tasks []CompressTask, vaAPIDevice string) {
	if vaAPIDevice != "" {
		fmt.Fprintf(dest, "Hardware acceleration: %s\n", vaAPIDevice)
	} else {
		fmt.Fprintln(dest, "Hardware acceleration: (software)")
	}
	w := tabwriter.NewWriter(dest, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "#\tInput\tOutput\tCodec\tCRF\tResolution")
	fmt.Fprintln(w, "-\t-----\t------\t-----\t---\t----------")
	for i, t := range tasks {
		res := t.Settings.Resolution
		if res == "" {
			res = "(keep)"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%s\n",
			i+1, t.InputPath, filepath.Base(t.OutputPath), t.Settings.Codec, t.Settings.CRF, res)
	}
	w.Flush()
	fmt.Fprintln(dest)
}

func buildTaskList(cfg *config.Config, rootDir string) ([]CompressTask, error) {
	var tasks []CompressTask
	err := walkItems(cfg.Items, rootDir, cfg.Defaults, nil, &tasks)
	return tasks, err
}

// walkItems recursively walks the config tree, resolving settings for each file.
// settingsStack holds the Settings of each ancestor directory, outermost first.
func walkItems(
	items map[string]*config.ItemNode,
	absDir string,
	defaults config.Settings,
	settingsStack []config.Settings,
	tasks *[]CompressTask,
) error {
	// Sort keys for deterministic output order
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		node := items[name]
		absPath := filepath.Join(absDir, name)

		if node.Items != nil {
			// Directory node: push its settings onto the stack and recurse
			newStack := make([]config.Settings, len(settingsStack)+1)
			copy(newStack, settingsStack)
			newStack[len(settingsStack)] = node.Settings

			if err := walkItems(node.Items, absPath, defaults, newStack, tasks); err != nil {
				return err
			}
		} else {
			// File node: resolve settings and add task if not skipped
			resolved, err := settings.ResolveForFile(defaults, settingsStack, node.Settings)
			if err != nil {
				return fmt.Errorf("%s: %w", absPath, err)
			}
			if resolved.Skip {
				continue
			}
			*tasks = append(*tasks, CompressTask{
				InputPath:  absPath,
				OutputPath: filename.CompressedOutputPath(absPath),
				Settings:   resolved,
			})
		}
	}
	return nil
}

// compressedOutputPath is kept as a package-level alias for backward compatibility.
// New callers should use filename.CompressedOutputPath directly.
func compressedOutputPath(inputPath string) string {
	return filename.CompressedOutputPath(inputPath)
}
