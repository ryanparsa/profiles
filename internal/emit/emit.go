// Package emit hands shell code back to the calling shell. With the hook
// installed, code goes to $PROFILE_EVAL_FILE, which the wrapper function
// sources; without it, code goes to stdout for `eval "$(profiles ...)"`.
package emit

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/term"
)

// EnvEvalFile is set by the wrapper function to a temp file it sources.
const EnvEvalFile = "PROFILE_EVAL_FILE"

// ErrNoHook means there is no hook and stdout is a terminal, so the code
// would only be printed, never run.
var ErrNoHook = errors.New("shell integration is not installed")

// Script collects shell code to run in the calling shell.
type Script struct{ lines []string }

// Add appends lines of code.
func (s *Script) Add(lines ...string) {
	for _, l := range lines {
		if l != "" {
			s.lines = append(s.lines, strings.TrimRight(l, "\n"))
		}
	}
}

// Empty reports whether nothing was added.
func (s *Script) Empty() bool { return len(s.lines) == 0 }

// String returns the code.
func (s *Script) String() string {
	if s.Empty() {
		return ""
	}
	return strings.Join(s.lines, "\n") + "\n"
}

// Flush writes the code to the eval file or stdout.
func (s *Script) Flush() error {
	if s.Empty() {
		return nil
	}
	if f := os.Getenv(EnvEvalFile); f != "" {
		fh, err := os.OpenFile(f, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		if _, err := fh.WriteString(s.String()); err != nil {
			fh.Close()
			return err
		}
		return fh.Close()
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return ErrNoHook
	}
	_, err := os.Stdout.WriteString(s.String())
	return err
}
