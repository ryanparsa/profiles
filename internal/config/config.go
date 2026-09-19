// Package config loads ~/.profiles/config.toml. Every key can also be set
// from the environment with a PROFILES_ prefix, e.g. PROFILES_QUIET=true or
// PROFILES_AUTOLOAD="default work".
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

// FileName is the config file inside the profiles directory.
const FileName = "config.toml"

// Default is written by `profiles config` when no config exists yet.
const Default = `# profiles configuration. Every key can also be set with an env var,
# e.g. PROFILES_QUIET=true or PROFILES_AUTOLOAD="default work".

# Profiles loaded in every new terminal (needs the init line in your rc file).
autoload = []

# Editor for "profiles new/edit/config". Empty uses $VISUAL, then $EDITOR.
editor = ""

# Ask before "profiles rm" deletes a profile.
confirm_delete = true

# Hide the "✓ loaded work" messages.
quiet = false
`

// Config holds the user's settings.
type Config struct {
	Autoload      []string `toml:"autoload"`
	Editor        string   `toml:"editor"`
	ConfirmDelete bool     `toml:"confirm_delete"`
	Quiet         bool     `toml:"quiet"`
}

// Path returns the config file path inside dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Load reads the config from dir, then applies PROFILES_* env overrides.
// A missing file yields the defaults.
func Load(dir string) (*Config, error) {
	c := &Config{ConfirmDelete: true}
	b, err := os.ReadFile(Path(dir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := toml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if err := c.applyEnv(); err != nil {
		return nil, err
	}
	c.Autoload = splitNames(c.Autoload)
	return c, nil
}

// applyEnv overrides settings from non-empty PROFILES_<KEY> env vars.
func (c *Config) applyEnv() error {
	if v := os.Getenv("PROFILES_AUTOLOAD"); v != "" {
		c.Autoload = []string{v}
	}
	if v := os.Getenv("PROFILES_EDITOR"); v != "" {
		c.Editor = v
	}
	for key, dst := range map[string]*bool{
		"PROFILES_CONFIRM_DELETE": &c.ConfirmDelete,
		"PROFILES_QUIET":          &c.Quiet,
	} {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s=%q: want true or false", key, v)
		}
		*dst = b
	}
	return nil
}

// splitNames splits entries on commas and whitespace, since from the
// environment autoload arrives as one string ("default work" or
// "default,work"). Profile names can't contain either separator.
func splitNames(entries []string) []string {
	var out []string
	for _, e := range entries {
		out = append(out, strings.FieldsFunc(e, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })...)
	}
	return out
}

// EnsureFile creates the default config file if it doesn't exist.
func EnsureFile(dir string) (string, error) {
	p := Path(dir)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return p, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(Default); err != nil {
		f.Close()
		return "", err
	}
	return p, f.Close()
}
