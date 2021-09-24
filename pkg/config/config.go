// Package config stores all devenv configuration
package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

type Config struct {
	// CurrentContext is the current devenv in use.
	CurrentContext string `yaml:"currentContext"`
}

// ParseContext returns the runtime and name of the current context
func (c *Config) ParseContext() (runtime, name string) {
	spl := strings.Split(c.CurrentContext, ":")
	if len(spl) != 2 {
		return "", ""
	}

	return spl[0], spl[1]
}

func getConfigFile() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to read user's home dir")
	}

	return filepath.Join(homeDir, ".config", "devenv", "config.yaml"), nil
}

// LoadConfig reads the config from disk
func LoadConfig(ctx context.Context) (*Config, error) {
	confPath, err := getConfigFile()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get config file path")
	}

	f, err := os.Open(confPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open config file for writing")
	}
	defer f.Close()

	var conf *Config
	err = json.NewDecoder(f).Decode(&conf)
	return conf, err
}

// SaveConfig saves a provided config to disk
func SaveConfig(_ context.Context, c *Config) error {
	confPath, err := getConfigFile()
	if err != nil {
		return errors.Wrap(err, "failed to get config file path")
	}

	f, err := os.Create(confPath)
	if err != nil {
		return errors.Wrap(err, "failed to open config file for writing")
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(c)
}
