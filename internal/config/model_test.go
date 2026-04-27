package config

import "testing"

func TestCopyReusableSettingsCopiesMatchingNodes(t *testing.T) {
	newConfig := &Config{
		Defaults: Settings{Quality: "normal", Codec: "h265"},
		Items: map[string]*ItemNode{
			"movie.mp4": {
				CompressedStatus: &CompressedStatus{CompressedRatio: "40%"},
			},
			"series": {
				Items: map[string]*ItemNode{
					"episode.mp4": {
						CompressedStatus: &CompressedStatus{Unfinished: true},
					},
				},
			},
			"new_only.mp4": {},
		},
	}

	oldConfig := &Config{
		Defaults: Settings{Quality: "high", Codec: "h264"},
		Items: map[string]*ItemNode{
			"movie.mp4": {
				Settings: Settings{
					Quality:    "high",
					Resolution: "1080p",
					Codec:      "h264",
					Tags:       "favorite",
					Skip:       true,
				},
				CompressedStatus: &CompressedStatus{CompressedRatio: "99%"},
			},
			"series": {
				Settings: Settings{
					Quality:    "low",
					Resolution: "720p",
					Codec:      "h265",
					Tags:       "tv",
					Skip:       true,
				},
				Items: map[string]*ItemNode{
					"episode.mp4": {
						Settings: Settings{
							Quality:    "normal",
							Resolution: "480p",
							Codec:      "h264",
							Tags:       "queued",
						},
						CompressedStatus: &CompressedStatus{CompressedRatio: "10%"},
					},
				},
			},
			"old_only.mp4": {
				Settings: Settings{Quality: "lossless"},
			},
		},
	}

	CopyReusableSettings(newConfig, oldConfig)

	movie := newConfig.Items["movie.mp4"]
	if movie == nil {
		t.Fatal("movie.mp4 should exist")
	}
	if movie.Quality != "high" || movie.Resolution != "1080p" || movie.Codec != "h264" || movie.Tags != "favorite" || !movie.Skip {
		t.Fatalf("movie.mp4 settings were not copied correctly: %+v", movie.Settings)
	}
	if movie.CompressedStatus == nil || movie.CompressedStatus.CompressedRatio != "40%" {
		t.Fatalf("movie.mp4 compressed status should be preserved from new config, got %+v", movie.CompressedStatus)
	}

	series := newConfig.Items["series"]
	if series == nil {
		t.Fatal("series directory should exist")
	}
	if series.Quality != "low" || series.Resolution != "720p" || series.Codec != "h265" || series.Tags != "tv" || !series.Skip {
		t.Fatalf("series directory settings were not copied correctly: %+v", series.Settings)
	}

	episode := series.Items["episode.mp4"]
	if episode == nil {
		t.Fatal("episode.mp4 should exist")
	}
	if episode.Quality != "normal" || episode.Resolution != "480p" || episode.Codec != "h264" || episode.Tags != "queued" {
		t.Fatalf("episode.mp4 settings were not copied correctly: %+v", episode.Settings)
	}
	if !episode.CompressedStatus.Unfinished {
		t.Fatalf("episode.mp4 compressed status should remain from new config, got %+v", episode.CompressedStatus)
	}

	newOnly := newConfig.Items["new_only.mp4"]
	if newOnly == nil {
		t.Fatal("new_only.mp4 should exist")
	}
	if newOnly.Settings != (Settings{}) {
		t.Fatalf("new_only.mp4 should be unchanged when no old node matches, got %+v", newOnly.Settings)
	}
}

func TestCopyReusableSettingsHandlesNilConfigs(t *testing.T) {
	tests := []struct {
		name            string
		newConfig       *Config
		oldConfig       *Config
		assertAfterCall func(t *testing.T, newConfig, oldConfig *Config)
	}{
		{
			name:      "nil new config",
			newConfig: nil,
			oldConfig: &Config{
				Items: map[string]*ItemNode{
					"video.mp4": {Settings: Settings{Quality: "high"}},
				},
			},
			assertAfterCall: func(t *testing.T, newConfig, oldConfig *Config) {
				t.Helper()
				if newConfig != nil {
					t.Fatalf("newConfig should remain nil, got %+v", newConfig)
				}
				if oldConfig == nil || oldConfig.Items["video.mp4"] == nil {
					t.Fatal("oldConfig should remain intact")
				}
				if oldConfig.Items["video.mp4"].Quality != "high" {
					t.Fatalf("oldConfig should be unchanged, got %+v", oldConfig.Items["video.mp4"].Settings)
				}
			},
		},
		{
			name: "nil old config",
			newConfig: &Config{
				Items: map[string]*ItemNode{
					"video.mp4": {Settings: Settings{Quality: "normal"}},
				},
			},
			oldConfig: nil,
			assertAfterCall: func(t *testing.T, newConfig, oldConfig *Config) {
				t.Helper()
				if oldConfig != nil {
					t.Fatalf("oldConfig should remain nil, got %+v", oldConfig)
				}
				if newConfig == nil || newConfig.Items["video.mp4"] == nil {
					t.Fatal("newConfig should remain intact")
				}
				if newConfig.Items["video.mp4"].Quality != "normal" {
					t.Fatalf("newConfig should be unchanged, got %+v", newConfig.Items["video.mp4"].Settings)
				}
			},
		},
		{
			name: "both nil",
			assertAfterCall: func(t *testing.T, newConfig, oldConfig *Config) {
				t.Helper()
				if newConfig != nil || oldConfig != nil {
					t.Fatalf("both configs should remain nil, got new=%+v old=%+v", newConfig, oldConfig)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			CopyReusableSettings(tc.newConfig, tc.oldConfig)
			tc.assertAfterCall(t, tc.newConfig, tc.oldConfig)
		})
	}
}

func TestCopyReusableItemSettingsSkipsUnusableNodes(t *testing.T) {
	newItems := map[string]*ItemNode{
		"matched.mp4": {
			CompressedStatus: &CompressedStatus{CompressedRatio: "45%"},
		},
		"missing_old.mp4": {},
		"mismatched_kind": {
			Items: map[string]*ItemNode{},
		},
		"nil_new.mp4": nil,
	}
	oldItems := map[string]*ItemNode{
		"matched.mp4": {
			Settings: Settings{Quality: "high", Resolution: "1080p"},
		},
		"mismatched_kind": {},
		"nil_new.mp4": {
			Settings: Settings{Codec: "h264"},
		},
	}

	copyReusableItemSettings(newItems, oldItems)

	matched := newItems["matched.mp4"]
	if matched.Quality != "high" || matched.Resolution != "1080p" {
		t.Fatalf("matched node settings were not copied: %+v", matched.Settings)
	}
	if matched.CompressedStatus == nil || matched.CompressedStatus.CompressedRatio != "45%" {
		t.Fatalf("matched node compressed status should be preserved, got %+v", matched.CompressedStatus)
	}

	if newItems["missing_old.mp4"].Settings != (Settings{}) {
		t.Fatalf("node without old match should be unchanged, got %+v", newItems["missing_old.mp4"].Settings)
	}
	if len(newItems["mismatched_kind"].Items) != 0 {
		t.Fatalf("mismatched node kind should not be modified, got %+v", newItems["mismatched_kind"])
	}
	if newItems["nil_new.mp4"] != nil {
		t.Fatalf("nil new node should remain nil, got %+v", newItems["nil_new.mp4"])
	}
}

func TestCopyReusableItemSettingsHandlesEmptyMaps(t *testing.T) {
	tests := []struct {
		name            string
		newItems        map[string]*ItemNode
		oldItems        map[string]*ItemNode
		assertAfterCall func(t *testing.T, newItems, oldItems map[string]*ItemNode)
	}{
		{
			name:     "nil new items",
			newItems: nil,
			oldItems: map[string]*ItemNode{"video.mp4": {Settings: Settings{Quality: "high"}}},
			assertAfterCall: func(t *testing.T, newItems, oldItems map[string]*ItemNode) {
				t.Helper()
				if newItems != nil {
					t.Fatalf("newItems should remain nil, got %+v", newItems)
				}
				if oldItems["video.mp4"] == nil || oldItems["video.mp4"].Quality != "high" {
					t.Fatalf("oldItems should be unchanged, got %+v", oldItems["video.mp4"])
				}
			},
		},
		{
			name: "nil old items",
			newItems: map[string]*ItemNode{
				"video.mp4": {Settings: Settings{Quality: "normal"}},
			},
			oldItems: nil,
			assertAfterCall: func(t *testing.T, newItems, oldItems map[string]*ItemNode) {
				t.Helper()
				if oldItems != nil {
					t.Fatalf("oldItems should remain nil, got %+v", oldItems)
				}
				if newItems["video.mp4"] == nil || newItems["video.mp4"].Quality != "normal" {
					t.Fatalf("newItems should be unchanged, got %+v", newItems["video.mp4"])
				}
			},
		},
		{
			name:     "both empty",
			newItems: map[string]*ItemNode{},
			oldItems: map[string]*ItemNode{},
			assertAfterCall: func(t *testing.T, newItems, oldItems map[string]*ItemNode) {
				t.Helper()
				if len(newItems) != 0 || len(oldItems) != 0 {
					t.Fatalf("both maps should remain empty, got new=%+v old=%+v", newItems, oldItems)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copyReusableItemSettings(tc.newItems, tc.oldItems)
			tc.assertAfterCall(t, tc.newItems, tc.oldItems)
		})
	}
}
