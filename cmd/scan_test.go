package cmd

import (
	"testing"

	"github.com/yushenli/video_compactor/internal/config"
)

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
