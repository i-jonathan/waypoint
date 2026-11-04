// Package config reads and writes the global Waypoint config file and keeps
// the registry of projects it lists.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"waypoint/internal/domain"
)

const defaultPort = 4747

// ProjectRef registers one board. Storage is "markdown" or "sqlite"; Path is
// absolute.
type ProjectRef struct {
	Name    string `yaml:"name"`
	Storage string `yaml:"storage"`
	Path    string `yaml:"path"`
}

// Config is the global configuration.
type Config struct {
	Port        int               `yaml:"port"`
	OpenBrowser bool              `yaml:"open_browser"`
	DataDir     string            `yaml:"data_dir"`
	Appearance  domain.Appearance `yaml:"appearance"`
	Projects    []ProjectRef      `yaml:"projects"`
}

// DefaultPath returns the config file location: $WAYPOINT_CONFIG, else
// $XDG_CONFIG_HOME/waypoint/config.yaml, else ~/.config/waypoint/config.yaml.
func DefaultPath(env func(string) string) (string, error) {
	if p := env("WAYPOINT_CONFIG"); p != "" {
		return p, nil
	}
	if x := env("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "waypoint", "config.yaml"), nil
	}
	home := env("HOME")
	if home == "" {
		return "", errors.New("cannot locate the config file: set WAYPOINT_CONFIG, XDG_CONFIG_HOME or HOME")
	}
	return filepath.Join(home, ".config", "waypoint", "config.yaml"), nil
}

// Load reads the config at path. A missing file gives the defaults and writes
// nothing. A file that exists but cannot be decoded, or that has an unknown
// key, is an error naming the file. Keys the file omits keep their defaults.
// env supplies HOME, used for the default data_dir and for a leading "~/".
func Load(path string, env func(string) string) (*Config, error) {
	home := env("HOME")
	c := &Config{Port: defaultPort, OpenBrowser: true}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// defaults only
	case err != nil:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	default:
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	if c.DataDir == "" {
		if home == "" {
			return nil, fmt.Errorf("config %s: HOME is not set and data_dir is not configured", path)
		}
		c.DataDir = filepath.Join(home, ".local", "share", "waypoint")
	}
	if c.DataDir, err = expandHome(c.DataDir, home); err != nil {
		return nil, fmt.Errorf("config %s: data_dir: %w", path, err)
	}
	for i := range c.Projects {
		if c.Projects[i].Path, err = expandHome(c.Projects[i].Path, home); err != nil {
			return nil, fmt.Errorf("config %s: project %q: %w", path, c.Projects[i].Name, err)
		}
	}
	return c, nil
}

// expandHome replaces a leading "~" or "~/" with home.
func expandHome(p, home string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	if home == "" {
		return "", fmt.Errorf("cannot expand %q: HOME is not set", p)
	}
	return filepath.Join(home, p[1:]), nil
}

// Save writes the config to path, creating parent directories. The write goes
// to a temp file in the same directory and is renamed into place.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
