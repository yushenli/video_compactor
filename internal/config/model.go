package config

const DefaultConfigFilename = "video_compactor.yaml"

// Settings holds the configurable fields that can appear at any level
// (defaults, directory node, or file node). All fields are optional in YAML.
type Settings struct {
	Quality    string `yaml:"quality,omitempty"`    // named preset (low/normal/high/lossless) or raw CRF int
	Resolution string `yaml:"resolution,omitempty"` // "720p","1080p","4k" OR raw "1920x1080","1280*720"
	Codec      string `yaml:"codec,omitempty"`      // "h264" or "h265"
	Tags       string `yaml:"tags,omitempty"`       // shorthand e.g. "normal,1080p" or "22,4k"
	Skip       bool   `yaml:"skip,omitempty"`
}

// ItemNode represents either a video file or a directory in the config tree.
// A directory node has a non-nil Items map; a file node has Items == nil.
type ItemNode struct {
	Settings `yaml:",inline"`
	Items    map[string]*ItemNode `yaml:"items,omitempty"`
}

// Config is the top-level YAML document structure.
type Config struct {
	Defaults Settings             `yaml:"defaults"`
	Items    map[string]*ItemNode `yaml:"items"`
}
