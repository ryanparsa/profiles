package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ryanparsa/profiles/internal/gitsync"
)

func linkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <git-url>",
		Short: "Sync ~/.profiles with a git repo",
		Long: `Link the profiles directory to a git remote.

On a new machine (no local profiles) the remote's profiles are checked out.
Otherwise the local profiles are committed and pushed to the remote, which
should be empty. Use a private repo: profiles often contain secrets.`,
		Args: cobra.ExactArgs(1),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			return link(a, args[0])
		}),
	}
}

func link(a *app, url string) error {
	r := gitsync.Repo{Dir: a.st.Dir}
	if !r.IsRepo() {
		if _, err := r.Output("init", "-q"); err != nil {
			return err
		}
		if _, err := r.Output("symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
			return err
		}
	}
	ignore := filepath.Join(a.st.Dir, ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(ignore, []byte(gitsync.GitIgnore), 0o600); err != nil {
			return err
		}
	}
	if err := r.SetRemote(url); err != nil {
		return err
	}
	branch, err := r.RemoteBranch()
	if err != nil {
		return err
	}

	if branch == "" { // empty remote: publish what we have
		if _, err := r.CommitAll("add profiles"); err != nil {
			return err
		}
		if err := r.Run("push", "-q", "-u", "origin", "HEAD"); err != nil {
			return err
		}
		a.info("✓ linked %s and pushed your profiles", url)
		return nil
	}

	if err := r.Run("fetch", "-q", "origin", branch); err != nil {
		return err
	}
	remote := "origin/" + branch
	profiles, err := a.st.List()
	if err != nil {
		return err
	}
	// Predefined profiles made on first use don't count as local profiles;
	// the checkout replaces them if the remote has its own.
	profiles = slices.DeleteFunc(profiles, a.untouched)
	hasCommits := r.HasCommits()
	switch {
	case !hasCommits && len(profiles) == 0: // new machine: take the remote
		if err := r.Run("checkout", "-q", "-f", "-B", branch, "--track", remote); err != nil {
			return err
		}
	case hasCommits && (isAncestor(r, remote, "HEAD") || isAncestor(r, "HEAD", remote)):
		if _, err := r.Output("branch", "--set-upstream-to="+remote); err != nil {
			return err
		}
	default:
		if _, err := r.CommitAll("add profiles"); err != nil {
			return err
		}
		return fmt.Errorf(`%w and so does this machine.
Merge them, then push:
  profiles git pull --no-rebase --allow-unrelated-histories origin %s
  profiles push`, gitsync.ErrRemoteHasHistory, branch)
	}
	if err := a.st.FixPerms(); err != nil {
		return err
	}
	a.info("✓ linked %s", url)
	return nil
}

func isAncestor(r gitsync.Repo, a, b string) bool {
	_, err := r.Output("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// linkedRepo returns the repo, or an error telling the user to link first.
func linkedRepo(a *app) (gitsync.Repo, error) {
	r := gitsync.Repo{Dir: a.st.Dir}
	if !r.IsRepo() || r.Remote() == "" {
		return r, errors.New("profiles aren't linked to a git repo yet; run: profiles link <git-url>")
	}
	return r, nil
}

func pushCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Commit all profile changes and push them",
		Args:  cobra.NoArgs,
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			r, err := linkedRepo(a)
			if err != nil {
				return err
			}
			if warned, _ := r.Output("config", "--get", "profile.secretsWarning"); warned == "" {
				warn("profiles often contain secrets. Make sure %s is a private repo.", r.Remote())
				if _, err := r.Output("config", "profile.secretsWarning", "shown"); err != nil {
					return err
				}
			}
			if message == "" {
				host, _ := os.Hostname()
				message = fmt.Sprintf("update profiles (%s)", host)
			}
			committed, err := r.CommitAll(message)
			if err != nil {
				return err
			}
			if r.HasUpstream() {
				err = r.Run("push", "-q")
			} else {
				err = r.Run("push", "-q", "-u", "origin", "HEAD")
			}
			if err != nil {
				return err
			}
			if committed {
				a.info("✓ committed and pushed")
			} else {
				a.info("✓ pushed (no new changes to commit)")
			}
			return nil
		}),
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	return cmd
}

func pullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull profile changes from the git remote",
		Args:  cobra.NoArgs,
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			r, err := linkedRepo(a)
			if err != nil {
				return err
			}
			if !r.HasUpstream() {
				branch, err := r.RemoteBranch()
				if err != nil {
					return err
				}
				if branch == "" {
					return errors.New("the remote is empty; nothing to pull (use profiles push)")
				}
				if err := r.Run("fetch", "-q", "origin", branch); err != nil {
					return err
				}
				if _, err := r.Output("branch", "--set-upstream-to=origin/"+branch); err != nil {
					return err
				}
			}
			before, _ := r.Output("rev-parse", "HEAD")
			if err := r.Run("pull", "-q", "--rebase", "--autostash"); err != nil {
				return fmt.Errorf("%w\nResolve it with: profiles git status", err)
			}
			if err := a.st.FixPerms(); err != nil {
				return err
			}
			after, _ := r.Output("rev-parse", "HEAD")
			if before == after {
				a.info("✓ already up to date")
				return nil
			}
			a.info("✓ pulled")
			if before == "" {
				return nil
			}
			changed, _ := r.Output("diff", "--name-only", before, after)
			files := strings.Split(changed, "\n")
			if s, err := a.state(); err == nil {
				for _, e := range s.Profiles {
					if slices.Contains(files, filepath.Base(e.File)) {
						a.info("%s changed and is loaded here; apply it with: profiles reload %s", e.Name, e.Name)
					}
				}
			}
			return nil
		}),
	}
}

func gitCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "git [args...]",
		Short:              "Run any git command in ~/.profiles",
		Example:            "  profiles git status\n  profiles git log --oneline",
		DisableFlagParsing: true,
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			c := exec.Command("git", append([]string{"-C", a.st.Dir}, args...)...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					return exitError(exit.ExitCode())
				}
				return err
			}
			return nil
		}),
	}
}
