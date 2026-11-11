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
	"slices"
	"strings"

	"github.com/gofrs/flock"
	"gopkg.in/yaml.v3"

	"waypoint/internal/dbconn"
	"waypoint/internal/domain"
)

const defaultPort = 4747

var tlsModes = []string{"auto", "on", "off"}

// ProjectRef registers one board. Storage is "markdown", "sqlite" or "postgres".
// A file project has an absolute Path. A server project has Host,
// Port, Database, User and SSL instead, and never a password: that comes from
// the environment (see package dbconn).
type ProjectRef struct {
	Name     string `yaml:"name"`
	Storage  string `yaml:"storage"`
	Path     string `yaml:"path,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Database string `yaml:"database,omitempty"`
	User     string `yaml:"user,omitempty"`
	SSL      string `yaml:"ssl,omitempty"`
}

// IsServer reports whether the project lives in a database server.
func (p ProjectRef) IsServer() bool { return p.Storage == "postgres" }

// Display is where the project lives, for people: its path, or
// host:port/database for a server. It never includes the user or a password.
func (p ProjectRef) Display() string {
	if !p.IsServer() {
		return p.Path
	}
	port := p.Port
	if port == 0 {
		port = dbconn.DefaultPort(p.Storage)
	}
	return fmt.Sprintf("%s:%d/%s", p.Host, port, p.Database)
}

// Config is the global configuration.
type Config struct {
	Port        int               `yaml:"port"`
	OpenBrowser bool              `yaml:"open_browser"`
	DataDir     string            `yaml:"data_dir"`
	TLS         string            `yaml:"tls"` // "auto", "on" or "off"
	Appearance  domain.Appearance `yaml:"appearance"`
	Projects    []ProjectRef      `yaml:"projects"`
}

// LoadError is a config file that exists but is wrong. Detail says how, without
// the path, so it can be shown to whoever fixes it.
type LoadError struct {
	Path   string
	Detail string
}

func (e *LoadError) Error() string { return fmt.Sprintf("config %s: %s", e.Path, e.Detail) }

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
// nothing. A file that exists but cannot be decoded, that has an unknown key,
// or whose tls is not auto, on or off, is an error naming the file. Keys the file omits keep their defaults.
// env supplies HOME, used for the default data_dir and for a leading "~/".
func Load(path string, env func(string) string) (*Config, error) {
	home := env("HOME")
	c := &Config{Port: defaultPort, OpenBrowser: true, TLS: "auto"}

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
			return nil, &LoadError{path, err.Error()}
		}
	}

	if !slices.Contains(tlsModes, c.TLS) {
		return nil, &LoadError{path, fmt.Sprintf("tls %q must be one of %s", c.TLS, strings.Join(tlsModes, ", "))}
	}

	if c.DataDir == "" {
		if home == "" {
			return nil, &LoadError{path, "HOME is not set and data_dir is not configured"}
		}
		c.DataDir = filepath.Join(home, ".local", "share", "waypoint")
	}
	if c.DataDir, err = expandHome(c.DataDir, home); err != nil {
		return nil, &LoadError{path, fmt.Sprintf("data_dir: %v", err)}
	}
	for i := range c.Projects {
		if c.Projects[i].Path, err = expandHome(c.Projects[i].Path, home); err != nil {
			return nil, &LoadError{path, fmt.Sprintf("project %q: %v", c.Projects[i].Name, err)}
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

// Update changes the config at path under an exclusive lock on <path>.lock: it
// loads the config, runs fn and saves. Every read-modify-write of the config
// goes through it, or concurrent writers would overwrite each other's changes.
// If fn fails nothing is saved. Update waits for as long as the lock is held,
// and the lock belongs to the operating system, so a crash releases it.
func Update(path string, env func(string) string, fn func(*Config) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	fl := flock.New(path + ".lock")
	if err := fl.Lock(); err != nil {
		return fmt.Errorf("lock config %s: %w", path, err)
	}
	defer fl.Unlock()

	c, err := Load(path, env)
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	return c.Save(path)
}

// Save writes the config to path, creating parent directories. The write goes
// to a temp file beside the real file and is renamed into place, so a config
// kept behind a symlink stays there. A dangling symlink is an error, rather than
// be replaced by a new file. Comments in the file are not kept.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if path, err = realPath(path); err != nil {
		return fmt.Errorf("write config: %w", err)
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

// realPath resolves the symlinks in path. A file that does not exist yet is left
// as it is, since the rename follows any symlinked directory by itself.
func realPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%s is a symlink to a file that does not exist", path)
	}
	return path, nil
}
