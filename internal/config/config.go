// Package config resolves where the source tree lives.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvCodeDir is the environment variable that overrides every other source.
const EnvCodeDir = "CODE_DIR"

// DefaultDirName is appended to the home directory when nothing else supplies
// a code directory.
const DefaultDirName = "codes"

// Config is the resolved configuration.
type Config struct {
	// CodeDir is the absolute root of the source tree, e.g. ~/go/src.
	CodeDir string
	// Source names where CodeDir came from: "env", "file" or "default".
	Source string
}

// Options are the inputs to [Resolve]. The zero value is not useful; use
// [Load], which fills them from the process environment.
type Options struct {
	// Getenv looks up an environment variable. Required.
	Getenv func(string) string
	// Home is the user's home directory, used to expand "~" and "$HOME".
	Home string
	// ConfigPath is the YAML file to read. Required; a missing file is not an
	// error.
	ConfigPath string
}

// file is the on-disk schema of ~/.config/p/config.yaml.
type file struct {
	CodeDir string `yaml:"code_dir"`
}

// Load resolves the configuration from the process environment.
func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("locate home directory: %w", err)
	}
	return Resolve(Options{
		Getenv:     os.Getenv,
		Home:       home,
		ConfigPath: DefaultConfigPath(home),
	})
}

// DefaultConfigPath returns ~/.config/p/config.yaml for the given home.
func DefaultConfigPath(home string) string {
	return filepath.Join(home, ".config", "p", "config.yaml")
}

// Resolve applies the resolution order: $CODE_DIR, then the config file, then
// $HOME/codes. Paths from any source have "~" and "$HOME" expanded and are
// made absolute.
func Resolve(opts Options) (Config, error) {
	if opts.Getenv == nil {
		return Config{}, errors.New("config: Getenv is required")
	}

	if raw := strings.TrimSpace(opts.Getenv(EnvCodeDir)); raw != "" {
		dir, err := Expand(raw, opts.Home, opts.Getenv)
		if err != nil {
			return Config{}, fmt.Errorf("expand $%s: %w", EnvCodeDir, err)
		}
		return Config{CodeDir: dir, Source: "env"}, nil
	}

	raw, err := readFile(opts.ConfigPath)
	if err != nil {
		return Config{}, err
	}
	if raw != "" {
		dir, err := Expand(raw, opts.Home, opts.Getenv)
		if err != nil {
			return Config{}, fmt.Errorf("expand code_dir from %s: %w", opts.ConfigPath, err)
		}
		return Config{CodeDir: dir, Source: "file"}, nil
	}

	return Config{CodeDir: filepath.Join(opts.Home, DefaultDirName), Source: "default"}, nil
}

// readFile returns the code_dir value from path. A missing file yields an
// empty string and no error.
func readFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	return strings.TrimSpace(f.CodeDir), nil
}

// Expand resolves a leading "~" and any $VAR references in path against home
// and getenv, then makes the result absolute.
func Expand(path, home string, getenv func(string) string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}

	path = os.Expand(path, func(key string) string {
		if key == "HOME" && home != "" {
			return home
		}
		return getenv(key)
	})

	switch {
	case path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}

	if path == "" {
		return "", errors.New("path expanded to empty string")
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("absolute path for %q: %w", path, err)
		}
		return abs, nil
	}
	return filepath.Clean(path), nil
}
