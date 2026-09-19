// Package cli defines the profile commands.
package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ryanparsa/profiles/internal/config"
	"github.com/ryanparsa/profiles/internal/emit"
	"github.com/ryanparsa/profiles/internal/shell"
	"github.com/ryanparsa/profiles/internal/state"
	"github.com/ryanparsa/profiles/internal/store"
)

// Version is set at build time with -ldflags "-X ...cli.Version=v1.2.3".
var Version = ""

var shellFlag string

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	err := newRoot().Execute()
	var exit exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return int(exit)
	case errors.Is(err, emit.ErrNoHook):
		printHookHelp()
	default:
		fmt.Fprintln(os.Stderr, "profiles:", err)
	}
	return 1
}

// exitError passes a child process's exit code through without printing
// anything, e.g. for "profiles git".
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func newRoot() *cobra.Command {
	var noShared bool
	root := &cobra.Command{
		Use:   "profiles [name...]",
		Short: "Load and unload env profiles in the current terminal",
		Long: `profiles manages shell profiles in ~/.profiles and loads them into the
current terminal. Everything a profile adds or changes (env vars, functions,
aliases) is undone again by "profiles unload".

Run "profiles" with no arguments to list profiles, or "profiles <name>" as
a shortcut for "profiles load <name>".`,
		Example: `  eval "$(profiles install zsh)"  # in ~/.zshrc
  profiles new work                # create ~/.profiles/work.sh
  profiles work                    # load it
  profiles status                  # what's loaded here
  profiles unload                  # unload everything`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeProfiles(false),
		Version:           version(),
		SilenceUsage:      true,
		SilenceErrors:     true,
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.list()
			}
			return a.load(args, false, !noShared)
		}),
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.Flags().BoolVar(&noShared, "no-shared", false, noSharedUsage)
	root.PersistentFlags().StringVar(&shellFlag, "shell", "", "shell to generate code for (zsh, bash, pwsh); detected by default")

	root.AddCommand(
		loadCmd(), unloadCmd(), reloadCmd(),
		listCmd(), statusCmd(), newCmd(), editCmd(), rmCmd(), showCmd(), diffCmd(), renameCmd(), cpCmd(),
		installCmd(), configCmd(),
		linkCmd(), pushCmd(), pullCmd(), gitCmd(),
		snapshotCmd(), recordCmd(), dumpenvCmd(), autoloadCmd(),
	)
	return root
}

func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

// app bundles what most commands need.
type app struct {
	sh  shell.Shell
	st  *store.Store
	cfg *config.Config
	s   *state.State // read on first use by state()
}

func newApp() (*app, error) {
	sh, err := shell.Detect(shellFlag)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(sh.Ext())
	if err != nil {
		return nil, err
	}
	_, err = os.Stat(config.Path(st.Dir))
	firstRun := errors.Is(err, fs.ErrNotExist)
	cfg, err := config.Load(st.Dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", config.Path(st.Dir), err)
	}
	a := &app{sh: sh, st: st, cfg: cfg}
	if firstRun {
		a.setup()
	}
	return a, nil
}

// predefined are the profiles setup creates, with their descriptions.
var predefined = []struct{ name, desc string }{
	{store.Default, `loaded in new terminals unless config.toml sets autoload`},
	{store.Shared, `loaded before every other profile (skip with --no-shared)`},
}

func predefinedContent(sh shell.Shell, name, desc string) string {
	return strings.Replace(sh.Template(name), "describe this profile here", desc, 1)
}

// setup runs on first use, when there is no config file yet: it creates the
// predefined profiles, then the config file. Once the config file exists,
// deleted predefined profiles stay deleted.
func (a *app) setup() {
	for _, p := range predefined {
		// Create fails if the profile exists, e.g. because another terminal
		// starting at the same time just created it.
		if !a.st.Exists(p.name) {
			if _, err := a.st.Create(p.name, predefinedContent(a.sh, p.name, p.desc)); err != nil && !a.st.Exists(p.name) {
				warn("creating %s: %v", p.name, err)
			}
		}
	}
	if _, err := config.EnsureFile(a.st.Dir); err != nil {
		warn("creating %s: %v", config.Path(a.st.Dir), err)
	}
}

// untouched reports whether p is a predefined profile still as setup
// created it.
func (a *app) untouched(p store.Profile) bool {
	for _, d := range predefined {
		if d.name == p.Name {
			b, err := os.ReadFile(p.Path)
			return err == nil && string(b) == predefinedContent(a.sh, d.name, d.desc)
		}
	}
	return false
}

// withApp adapts fn into a cobra RunE that builds the app first.
func withApp(fn func(a *app, cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a, err := newApp()
		if err != nil {
			return err
		}
		return fn(a, cmd, args)
	}
}

// state returns the profiles loaded in this terminal.
func (a *app) state() (*state.State, error) {
	if a.s == nil {
		s, err := state.Load()
		if err != nil {
			return nil, err
		}
		a.s = s
	}
	return a.s, nil
}

// info prints a status message unless quiet is set.
func (a *app) info(format string, args ...any) {
	if !a.cfg.Quiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "profiles: "+format+"\n", args...)
}

func printHookHelp() {
	sh, err := shell.Detect(shellFlag)
	if err != nil {
		sh, _ = shell.Get("zsh")
	}
	fmt.Fprintf(os.Stderr, `profiles: shell integration is not installed, so this terminal can't be changed.

Add this line to %s and open a new terminal:
  %s

Or run a single command without it:
  %s
`, sh.RCFile(), sh.InitLine(), sh.EvalLine("load <name>"))
}

// reserved returns names that can't be used as profiles because
// "profiles <name>" would run a command instead.
func reserved(root *cobra.Command) []string {
	names := []string{"help"}
	for _, c := range root.Commands() {
		names = append(names, c.Name())
		names = append(names, c.Aliases...)
	}
	return names
}

func validateNewName(cmd *cobra.Command, name string) error {
	if err := store.ValidateName(name); err != nil {
		return err
	}
	if slices.Contains(reserved(cmd.Root()), name) {
		return fmt.Errorf("%q is a profile command; pick another name", name)
	}
	return nil
}

// completeProfiles completes profile names (only loaded ones if loaded).
func completeProfiles(loaded bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if loaded {
			s, err := state.Load()
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var names []string
			for _, e := range s.Profiles {
				names = append(names, e.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		}
		sh, err := shell.Detect(shellFlag)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		dir, err := store.Dir()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		profiles, _ := (&store.Store{Dir: dir, Ext: sh.Ext()}).List()
		var names []string
		for _, p := range profiles {
			if !slices.Contains(args, p.Name) {
				names = append(names, cobra.CompletionWithDesc(p.Name, p.Desc))
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// editorArgv returns the editor command: configured, then $VISUAL, then
// $EDITOR, then the OS default. Blank values count as unset.
func editorArgv(configured string) []string {
	editor := strings.TrimSpace(configured)
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if editor == "" {
			editor = strings.TrimSpace(os.Getenv(env))
		}
	}
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	// A path with spaces, like C:\Program Files\...\code.exe, is one word.
	if _, err := exec.LookPath(editor); err == nil {
		return []string{editor}
	}
	return strings.Fields(editor) // e.g. "code --wait"
}

// openEditor opens path in the configured editor.
func (a *app) openEditor(path string) error {
	argv := editorArgv(a.cfg.Editor)
	editor := strings.Join(argv, " ")
	c := exec.Command(argv[0], append(argv[1:], path)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	if !isTerminal(os.Stdout) { // e.g. inside eval "$(profiles)"
		c.Stdout = os.Stderr
	}
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor %q: %w", editor, err)
	}
	return nil
}
