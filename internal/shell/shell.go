// Package shell generates the code each supported shell runs: the `profiles`
// wrapper function, and the load/unload snippets it sources.
package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ryanparsa/profiles/internal/state"
)

// EnvShell is set by the wrapper function so the binary knows its shell.
const EnvShell = "PROFILE_SHELL"

// UnloadFunc is the optional function a profile defines to run on unload.
const UnloadFunc = "profile_unload"

// Shell is one supported shell.
type Shell interface {
	// Name is the shell's name as accepted by `profiles install`.
	Name() string
	// Ext is the profile file extension for this shell.
	Ext() string
	// Init returns the rc-file hook: the wrapper function, then completion,
	// then "profiles <autoload>" unless autoload is empty.
	Init(completion, autoload string) string
	// Load returns code that snapshots the shell, sources file, and records
	// what changed under name. quiet suppresses the "loaded" message.
	Load(name, file string, quiet bool) string
	// Op renders one restore operation.
	Op(op state.Op) string
	// SetState stores the encoded state ("" clears it).
	SetState(encoded string) string
	// Command returns argv to source file in a fresh shell and then run
	// `exe args...`, used by `profiles diff`.
	Command(file, exe string, args ...string) []string
	// Template is the content of a new profile called name.
	Template(name string) string
	// RCFile is the user's rc file, as shown in help text.
	RCFile() string
	// InitLine is the rc-file line that installs the integration.
	InitLine() string
	// EvalLine runs `profiles args` once without the integration.
	EvalLine(args string) string
}

// Names lists the shells `profiles install` accepts.
var Names = []string{"zsh", "bash", "pwsh"}

// Get returns the Shell called name.
func Get(name string) (Shell, error) {
	switch strings.ToLower(name) {
	case "zsh":
		return posix{zsh: true}, nil
	case "bash":
		return posix{}, nil
	case "pwsh", "powershell":
		return pwsh{}, nil
	}
	return nil, fmt.Errorf("unsupported shell %q (supported: %s)", name, strings.Join(Names, ", "))
}

// Detect picks the shell: explicit flag, then $PROFILE_SHELL (set by the
// hook), then the OS default / $SHELL.
func Detect(flag string) (Shell, error) {
	if flag != "" {
		return Get(flag)
	}
	if s := os.Getenv(EnvShell); s != "" {
		return Get(s)
	}
	if runtime.GOOS == "windows" {
		return pwsh{}, nil
	}
	if s, err := Get(filepath.Base(os.Getenv("SHELL"))); err == nil {
		return s, nil
	}
	if runtime.GOOS == "darwin" {
		return posix{zsh: true}, nil
	}
	return posix{}, nil
}

// HookName is the name a profile's unload function is renamed to after
// loading, so stacked profiles don't overwrite each other's hook. The
// escaping is one-to-one ("_"→"__", "-"→"_d", "."→"_p"), so profiles like
// "a-b" and "a_b" get different hooks.
func HookName(profile string) string {
	return "__profile_unload_" + hookEscaper.Replace(profile)
}

var hookEscaper = strings.NewReplacer("_", "__", "-", "_d", ".", "_p")
