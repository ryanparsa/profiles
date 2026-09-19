// Package state tracks what each loaded profile changed in the current
// terminal. The state lives in the __PROFILE_STATE environment variable, so
// it is per-terminal, inherited by subshells, and never goes stale on disk.
package state

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

const (
	// EnvState holds the encoded State of the current terminal.
	EnvState = "__PROFILE_STATE"
	// EnvSnap carries the pre-load Snapshot from __snapshot to __record.
	EnvSnap = "__PROFILE_SNAP"
)

// ignored vars are never attributed to a profile.
var ignored = map[string]bool{
	EnvState:            true,
	EnvSnap:             true,
	"PROFILE_EVAL_FILE": true,
	"PROFILE_SHELL":     true,
	"_":                 true,
	"PWD":               true,
	"OLDPWD":            true,
	"SHLVL":             true,
}

var validKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Tracked reports whether changes to the env var key are recorded.
func Tracked(key string) bool {
	return !ignored[key] && validKey.MatchString(key)
}

// Snapshot is the environment, function and alias names of a shell at one
// point in time.
type Snapshot struct {
	Env     map[string]string `json:"e"`
	Funcs   []string          `json:"f,omitempty"`
	Aliases []string          `json:"a,omitempty"`
}

// VarChange is one env var a profile touched. Old is nil when the profile
// added the var; New is nil when the profile removed it.
type VarChange struct {
	Key string  `json:"k"`
	Old *string `json:"o,omitempty"`
	New *string `json:"n,omitempty"`
}

// Entry is everything one loaded profile changed.
type Entry struct {
	Name    string      `json:"n"`
	File    string      `json:"f"`
	Vars    []VarChange `json:"v,omitempty"`
	Funcs   []string    `json:"fn,omitempty"`
	Aliases []string    `json:"al,omitempty"`
	Hook    string      `json:"h,omitempty"`
}

// State is the ordered stack of loaded profiles, oldest first.
type State struct {
	Profiles []Entry `json:"p"`
}

// Load reads the State of the current terminal.
func Load() (*State, error) {
	s := &State{}
	raw := os.Getenv(EnvState)
	if raw == "" {
		return s, nil
	}
	if err := decode(raw, s); err != nil {
		return nil, fmt.Errorf("corrupt %s (run `unset %s` to reset): %w", EnvState, EnvState, err)
	}
	return s, nil
}

// Encode returns the value to store in __PROFILE_STATE, or "" when empty.
func (s *State) Encode() string {
	if len(s.Profiles) == 0 {
		return ""
	}
	return encode(s)
}

// Find returns the loaded entry named name.
func (s *State) Find(name string) (*Entry, bool) {
	for i := range s.Profiles {
		if s.Profiles[i].Name == name {
			return &s.Profiles[i], true
		}
	}
	return nil, false
}

// Loaded reports whether a profile named name is loaded.
func (s *State) Loaded(name string) bool {
	_, ok := s.Find(name)
	return ok
}

// Remove drops the entry named name.
func (s *State) Remove(name string) {
	s.Profiles = slices.DeleteFunc(s.Profiles, func(e Entry) bool { return e.Name == name })
}

// EncodeSnapshot serializes a Snapshot for __PROFILE_SNAP.
func EncodeSnapshot(s *Snapshot) string { return encode(s) }

// DecodeSnapshot parses a value produced by EncodeSnapshot.
func DecodeSnapshot(raw string) (*Snapshot, error) {
	s := &Snapshot{}
	if err := decode(raw, s); err != nil {
		return nil, err
	}
	return s, nil
}

func encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only plain data types are encoded
	}
	return base64.RawStdEncoding.EncodeToString(b)
}

func decode(raw string, v any) error {
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Environ returns the current process environment as a map.
func Environ() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" { // Windows has "=C:=C:\" style entries
			continue
		}
		env[k] = v
	}
	return env
}

// ParseNames reads "f name" / "a name" lines as emitted by the shell hooks.
func ParseNames(lines string) (funcs, aliases []string) {
	for line := range strings.Lines(lines) {
		kind, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		switch kind {
		case "f":
			funcs = append(funcs, name)
		case "a":
			aliases = append(aliases, name)
		}
	}
	return funcs, aliases
}
