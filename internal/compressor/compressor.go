package compressor

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/yushenli/video_compactor/internal/config"
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
	MaxJobs int
	DryRun  bool
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

	maxJobs := opts.MaxJobs
	if maxJobs < 1 {
		maxJobs = 1
	}

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
			args := BuildFFmpegArgs(t.InputPath, t.OutputPath, t.Settings)
			if err := ExecuteFFmpeg(args, opts.DryRun); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(task)
	}

	wg.Wait()
	return firstErr
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
				OutputPath: compressedOutputPath(absPath),
				Settings:   resolved,
			})
		}
	}
	return nil
}

// compressedOutputPath returns the output path for a given input.
// "video.mp4" → "video.compressed.mp4"
func compressedOutputPath(inputPath string) string {
	ext := filepath.Ext(inputPath)
	stem := strings.TrimSuffix(inputPath, ext)
	return stem + ".compressed" + ext
}
