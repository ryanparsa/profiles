// Package store manages the profile files in ~/.profiles.
package store

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// EnvDir overrides the profiles directory.
const EnvDir = "PROFILES_DIR"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrNotFound is returned for a profile that doesn't exist.
var ErrNotFound = errors.New("profile not found")

// Store is a directory of profile files with one extension (.sh or .ps1).
type Store struct {
	Dir string
	Ext string
}

// Profile is one profile file.
type Profile struct {
	Name string
	Path string
	Desc string
}

// Dir returns the profiles directory: $PROFILES_DIR or ~/.profiles.
func Dir() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".profiles"), nil
}

// Open returns the Store for ext, creating the directory (0700) if needed.
func Open(ext string) (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir, Ext: ext}, nil
}

// ValidateName checks a profile name is safe to use as a file name.
func ValidateName(name string) error {
	if !validName.MatchString(name) || strings.HasSuffix(name, ".") {
		return fmt.Errorf("invalid profile name %q: use letters, digits, '.', '_' and '-'", name)
	}
	return nil
}

// Path returns the file path for a profile name.
func (s *Store) Path(name string) string {
	return filepath.Join(s.Dir, name+s.Ext)
}

// Exists reports whether a profile exists.
func (s *Store) Exists(name string) bool {
	info, err := os.Stat(s.Path(name))
	return err == nil && info.Mode().IsRegular()
}

// Get returns an existing profile.
func (s *Store) Get(name string) (Profile, error) {
	if err := ValidateName(name); err != nil {
		return Profile{}, err
	}
	if !s.Exists(name) {
		return Profile{}, fmt.Errorf("%w: %s (looked for %s)", ErrNotFound, name, s.Path(name))
	}
	p := s.Path(name)
	return Profile{Name: name, Path: p, Desc: describe(p)}, nil
}

// List returns all profiles, sorted by name.
func (s *Store) List() ([]Profile, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), s.Ext)
		if !ok || !e.Type().IsRegular() || ValidateName(name) != nil {
			continue
		}
		p := filepath.Join(s.Dir, e.Name())
		out = append(out, Profile{Name: name, Path: p, Desc: describe(p)})
	}
	slices.SortFunc(out, func(a, b Profile) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// Create writes a new profile with mode 0600. It fails if the profile exists.
func (s *Store) Create(name, content string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	p := s.Path(name)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("profile %q already exists", name)
		}
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	return p, f.Close()
}

// Remove deletes a profile.
func (s *Store) Remove(name string) error {
	if _, err := s.Get(name); err != nil {
		return err
	}
	return os.Remove(s.Path(name))
}

// Rename moves a profile to a new name.
func (s *Store) Rename(from, to string, force bool) error {
	if err := s.checkTarget(from, to, force); err != nil {
		return err
	}
	return os.Rename(s.Path(from), s.Path(to))
}

// Copy duplicates a profile under a new name.
func (s *Store) Copy(from, to string, force bool) error {
	if err := s.checkTarget(from, to, force); err != nil {
		return err
	}
	// Opening dst truncates it, which would empty src if they're the same
	// file (same name, or a case variant on a case-insensitive filesystem).
	if s.sameFile(from, to) {
		return fmt.Errorf("%q and %q are the same profile", from, to)
	}
	src, err := os.Open(s.Path(from))
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(s.Path(to), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func (s *Store) checkTarget(from, to string, force bool) error {
	if _, err := s.Get(from); err != nil {
		return err
	}
	if err := ValidateName(to); err != nil {
		return err
	}
	if s.Exists(to) && !force {
		return fmt.Errorf("profile %q already exists (use --force to overwrite)", to)
	}
	return nil
}

func (s *Store) sameFile(a, b string) bool {
	ai, err := os.Stat(s.Path(a))
	if err != nil {
		return false
	}
	bi, err := os.Stat(s.Path(b))
	return err == nil && os.SameFile(ai, bi)
}

// FixPerms sets every profile file to 0600 (git doesn't keep modes).
func (s *Store) FixPerms() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for _, ext := range []string{".sh", ".ps1"} {
		matches, _ := filepath.Glob(filepath.Join(s.Dir, "*"+ext))
		for _, m := range matches {
			if err := os.Chmod(m, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

// describe returns the first comment line of a profile, used as its
// description in list and tab completion.
func describe(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#!") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		return ""
	}
	return ""
}
