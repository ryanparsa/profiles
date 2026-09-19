// Package gitsync syncs the profiles directory with a git remote using the
// system git, so the user's SSH keys and credential helpers just work.
package gitsync

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// GitIgnore keeps machine-only profiles out of the repo.
const GitIgnore = "*.local.sh\n*.local.ps1\n"

// Repo is a git working tree.
type Repo struct{ Dir string }

// IsRepo reports whether dir is a git repository.
func (r Repo) IsRepo() bool {
	_, err := os.Stat(filepath.Join(r.Dir, ".git"))
	return err == nil
}

// Run runs git with output shown to the user (stdout goes to stderr so it
// never mixes with code emitted for eval).
func (r Repo) Run(args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Output runs git and returns its trimmed stdout.
func (r Repo) Output(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasCommits reports whether HEAD points at a commit.
func (r Repo) HasCommits() bool {
	_, err := r.Output("rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// CommitAll stages everything and commits if anything changed. It reports
// whether a commit was made.
func (r Repo) CommitAll(msg string) (bool, error) {
	if _, err := r.Output("add", "-A"); err != nil {
		return false, err
	}
	if _, err := r.Output("diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	if _, err := r.Output("commit", "-q", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// HasUpstream reports whether the current branch tracks a remote branch.
func (r Repo) HasUpstream() bool {
	_, err := r.Output("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	return err == nil
}

// RemoteBranch returns the default branch of origin, or "" if the remote
// has no commits yet.
func (r Repo) RemoteBranch() (string, error) {
	out, err := r.Output("ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(out, "\n") {
		if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			return strings.Fields(ref)[0], nil
		}
	}
	// Some servers don't advertise HEAD; fall back to any branch.
	out, err = r.Output("ls-remote", "--heads", "origin")
	if err != nil || out == "" {
		return "", err
	}
	_, ref, _ := strings.Cut(strings.Split(out, "\n")[0], "refs/heads/")
	return ref, nil
}

// SetRemote points origin at url, adding it if needed.
func (r Repo) SetRemote(url string) error {
	if _, err := r.Output("remote", "get-url", "origin"); err == nil {
		_, err = r.Output("remote", "set-url", "origin", url)
		return err
	}
	_, err := r.Output("remote", "add", "origin", url)
	return err
}

// Remote returns origin's URL, or "" if there is none.
func (r Repo) Remote() string {
	out, _ := r.Output("remote", "get-url", "origin")
	return out
}

// Status is a short, offline summary of the working tree.
type Status struct {
	Ahead, Behind int
	Dirty         bool
}

var aheadBehind = regexp.MustCompile(`(ahead|behind) (\d+)`)

// Status returns ahead/behind counts from the last fetch plus local changes.
func (r Repo) Status() (Status, error) {
	out, err := r.Output("status", "--porcelain=v1", "-b")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out), nil
}

// parseStatus reads `git status --porcelain=v1 -b` output.
func parseStatus(out string) Status {
	lines := strings.Split(out, "\n")
	var s Status
	for _, m := range aheadBehind.FindAllStringSubmatch(lines[0], -1) {
		n, _ := strconv.Atoi(m[2]) // the regexp only matches digits
		if m[1] == "ahead" {
			s.Ahead = n
		} else {
			s.Behind = n
		}
	}
	s.Dirty = len(lines) > 1
	return s
}

func (s Status) String() string {
	var parts []string
	if s.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", s.Behind))
	}
	if s.Dirty {
		parts = append(parts, "uncommitted changes")
	}
	if len(parts) == 0 {
		return "up to date"
	}
	return strings.Join(parts, ", ")
}

// ErrRemoteHasHistory is returned by link when both sides already have
// profiles and would need a manual merge.
var ErrRemoteHasHistory = errors.New("remote already has profiles")
