package config

import (
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadConfig reads and unmarshals the YAML config file at path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig marshals cfg to YAML and writes it to path.
func SaveConfig(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeFileWithTempCopy(path, data)
}

func writeFileWithTempCopy(path string, data []byte) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	tempPath := tempFile.Name()
	tempClosed := false
	defer func() {
		if !tempClosed {
			_ = tempFile.Close()
		}
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(data); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	tempClosed = true

	sourceFile, err := os.Open(tempPath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	_, err = io.Copy(targetFile, sourceFile)
	return err
}
