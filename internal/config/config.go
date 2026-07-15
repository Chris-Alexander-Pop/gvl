package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the local CLI config (~/.config/gvl/config.yaml).
type Config struct {
	URL     string `yaml:"url"`     // daemon base URL, e.g. http://gvl.example.ts.net
	Token   string `yaml:"token"`   // bearer token
	Address string `yaml:"address"` // default device IP for direct LAN
}

// Dir returns the config directory.
func Dir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gvl")
}

// Path returns the config file path.
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

// Load reads config, applying env overrides.
func Load() (Config, error) {
	var c Config
	b, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if err == nil {
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, err
		}
	}
	if v := os.Getenv("GVL_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("GVL_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("GVL_DEVICE_IP"); v != "" {
		c.Address = v
	}
	return c, nil
}

// Save writes config to disk.
func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), b, 0o600)
}
