package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ryanparsa/profiles/internal/emit"
	"github.com/ryanparsa/profiles/internal/shell"
	"github.com/ryanparsa/profiles/internal/state"
	"github.com/ryanparsa/profiles/internal/store"
)

const noSharedUsage = `don't also load the "shared" profile`

func loadCmd() *cobra.Command {
	var noShared bool
	cmd := &cobra.Command{
		Use:   "load <name>...",
		Short: `Load profiles into this terminal (same as "profiles <name>")`,
		Long: `Load profiles into this terminal.

The "shared" profile, if it exists and isn't loaded yet, is loaded first so
the named profiles can override it. Use --no-shared to skip it.`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			return a.load(args, false, !noShared)
		}),
	}
	cmd.Flags().BoolVar(&noShared, "no-shared", false, noSharedUsage)
	return cmd
}

func unloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "unload [name...]",
		Short:             "Unload profiles (all of them if no name is given)",
		ValidArgsFunction: completeProfiles(true),
		RunE:              withApp(func(a *app, cmd *cobra.Command, args []string) error { return a.unload(args) }),
	}
}

func reloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "reload [name...]",
		Short:             "Unload and load profiles again (all loaded ones if no name is given)",
		ValidArgsFunction: completeProfiles(true),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				s, err := a.state()
				if err != nil {
					return err
				}
				for _, e := range s.Profiles {
					args = append(args, e.Name)
				}
				if len(args) == 0 {
					return errors.New("no profiles loaded")
				}
			}
			return a.load(args, false, false)
		}),
	}
}

// load emits code to load each profile. A profile that is already
// loaded is unloaded first, so loading it again is a reload. Repeated names
// load once: a second load would record the first one's changes as the
// starting point and never undo them. With shared, the shared profile goes
// first if it exists and isn't loaded yet.
func (a *app) load(names []string, quiet, shared bool) error {
	names = unique(names)
	s, err := a.state()
	if err != nil {
		return err
	}
	if shared && !slices.Contains(names, store.Shared) && !s.Loaded(store.Shared) && a.st.Exists(store.Shared) {
		names = append([]string{store.Shared}, names...)
	}
	// Resolve everything first so a typo doesn't half-load.
	profiles := make([]store.Profile, len(names))
	for i, n := range names {
		if profiles[i], err = a.st.Get(n); err != nil {
			return err
		}
	}
	env := state.Environ()
	var script emit.Script
	for _, p := range profiles {
		if e, ok := s.Find(p.Name); ok {
			a.unloadEntry(e, s, env, &script, true)
		}
		script.Add(a.sh.Load(p.Name, p.Path, quiet || a.cfg.Quiet))
	}
	return script.Flush()
}

// unload emits code to unload the named profiles, or all of them (newest
// first) when names is empty.
func (a *app) unload(names []string) error {
	names = unique(names)
	s, err := a.state()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		for i := len(s.Profiles) - 1; i >= 0; i-- {
			names = append(names, s.Profiles[i].Name)
		}
		if len(names) == 0 {
			a.info("No profiles loaded.")
			return nil
		}
	}
	for _, n := range names {
		if !s.Loaded(n) {
			return fmt.Errorf("%s is not loaded", n)
		}
	}
	env := state.Environ()
	var script emit.Script
	for _, n := range names {
		e, _ := s.Find(n)
		a.unloadEntry(e, s, env, &script, false)
	}
	return script.Flush()
}

// unloadEntry adds the code that undoes e and drops it from s.
func (a *app) unloadEntry(e *state.Entry, s *state.State, env map[string]string, script *emit.Script, silent bool) {
	name := e.Name
	ops, warnings := state.Restore(e, env)
	for _, op := range ops {
		script.Add(a.sh.Op(op))
	}
	for _, w := range warnings {
		warn("%s", w)
	}
	s.Remove(name)
	script.Add(a.sh.SetState(s.Encode()))
	if !silent {
		a.info("✓ unloaded %s", name)
	}
}

// Hidden commands used by the generated shell code.

func snapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__snapshot",
		Short:  "Print an encoded snapshot of the env plus function/alias names from stdin",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			snap, err := readSnapshot(os.Stdin)
			if err != nil {
				return err
			}
			fmt.Println(state.EncodeSnapshot(snap))
			return nil
		},
	}
}

func dumpenvCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__dumpenv",
		Short:  "Print an encoded snapshot of the env",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(state.EncodeSnapshot(&state.Snapshot{Env: trackedEnv()}))
			return nil
		},
	}
}

func recordCmd() *cobra.Command {
	var quiet bool
	cmd := &cobra.Command{
		Use:    "__record <name> <file>",
		Short:  "Record what loading a profile changed and print the new state",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, file := args[0], args[1]
			sh, err := shell.Detect(shellFlag)
			if err != nil {
				return err
			}
			before, err := state.DecodeSnapshot(os.Getenv(state.EnvSnap))
			if err != nil {
				return fmt.Errorf("missing snapshot: %w", err)
			}
			after, err := readSnapshot(os.Stdin)
			if err != nil {
				return err
			}
			s, err := state.Load()
			if err != nil {
				return err
			}
			e := state.Diff(name, file, shell.HookName(name), before, after)
			s.Remove(name)
			s.Profiles = append(s.Profiles, e)
			fmt.Println(sh.SetState(s.Encode()))

			if !quiet {
				fmt.Fprintf(os.Stderr, "✓ loaded %s%s\n", name, summary(&e))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "don't print the loaded message")
	return cmd
}

func autoloadCmd() *cobra.Command {
	var noShared bool
	cmd := &cobra.Command{
		Use:    "__autoload",
		Short:  "Load the profiles listed in config autoload, plus shared",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			s, err := a.state()
			if err != nil {
				return err
			}
			var names []string
			for _, n := range a.cfg.Autoload {
				switch {
				case s.Loaded(n):
				case n == store.Default && !a.st.Exists(n): // deleted on purpose
				case !a.st.Exists(n):
					warn("autoload: no profile named %q in %s", n, a.st.Dir)
				default:
					names = append(names, n)
				}
			}
			return a.load(names, true, !noShared)
		}),
	}
	cmd.Flags().BoolVar(&noShared, "no-shared", false, noSharedUsage)
	return cmd
}

// unique returns names without repeats, keeping the first occurrence.
func unique(names []string) []string {
	var out []string
	for _, n := range names {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func readSnapshot(r io.Reader) (*state.Snapshot, error) {
	in, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	funcs, aliases := state.ParseNames(string(in))
	return &state.Snapshot{Env: trackedEnv(), Funcs: funcs, Aliases: aliases}, nil
}

func trackedEnv() map[string]string {
	env := state.Environ()
	for k := range env {
		if !state.Tracked(k) {
			delete(env, k)
		}
	}
	return env
}

// summary describes an entry, e.g. " (+2 vars, ~1 var, 1 function)".
func summary(e *state.Entry) string {
	var added, changed, removed int
	for _, v := range e.Vars {
		switch {
		case v.Old == nil:
			added++
		case v.New == nil:
			removed++
		default:
			changed++
		}
	}
	var parts []string
	count := func(prefix string, n int, one, many string) {
		if n == 0 {
			return
		}
		word := one
		if n != 1 {
			word = many
		}
		parts = append(parts, fmt.Sprintf("%s%d %s", prefix, n, word))
	}
	count("+", added, "var", "vars")
	count("~", changed, "var", "vars")
	count("-", removed, "var", "vars")
	count("", len(e.Funcs), "function", "functions")
	count("", len(e.Aliases), "alias", "aliases")
	if len(parts) == 0 {
		return " (no changes)"
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
