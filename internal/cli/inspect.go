package cli

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/ryanparsa/profiles/internal/gitsync"
	"github.com/ryanparsa/profiles/internal/state"
)

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List profiles (● = loaded in this terminal)",
		Args:    cobra.NoArgs,
		RunE:    withApp(func(a *app, cmd *cobra.Command, args []string) error { return a.list() }),
	}
}

// list is "profiles list", and also the bare "profiles".
func (a *app) list() error {
	profiles, err := a.st.List()
	if err != nil {
		return err
	}
	// Piped: bare names, for scripts.
	if !isTerminal(os.Stdout) {
		for _, p := range profiles {
			fmt.Println(p.Name)
		}
		return nil
	}
	if len(profiles) == 0 {
		fmt.Printf("No profiles in %s yet. Create one with: profiles new <name>\n", a.st.Dir)
		return nil
	}
	s, err := a.state()
	if err != nil {
		return err
	}
	width := 0
	for _, p := range profiles {
		width = max(width, len(p.Name))
	}
	for _, p := range profiles {
		mark := "  "
		if s.Loaded(p.Name) {
			mark = green("● ")
		}
		fmt.Printf("%s%-*s  %s\n", mark, width, p.Name, dim(p.Desc))
	}
	a.printGitStatus()
	return nil
}

func (a *app) printGitStatus() {
	r := gitsync.Repo{Dir: a.st.Dir}
	if !r.IsRepo() {
		return
	}
	st, err := r.Status()
	if err != nil {
		return
	}
	remote := r.Remote()
	if remote == "" {
		remote = "no remote"
	}
	fmt.Println(dim(fmt.Sprintf("\ngit: %s (%s)", st, remote)))
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the profiles loaded in this terminal and what they changed",
		Args:  cobra.NoArgs,
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			s, err := a.state()
			if err != nil {
				return err
			}
			if len(s.Profiles) == 0 {
				fmt.Println("No profiles loaded.")
			}
			for i, e := range s.Profiles {
				if i > 0 {
					fmt.Println()
				}
				fmt.Printf("%s %s\n", green("● "+e.Name), dim(e.File))
				printVars(e.Vars)
				if len(e.Funcs) > 0 {
					fmt.Printf("  functions: %s\n", strings.Join(e.Funcs, ", "))
				}
				if len(e.Aliases) > 0 {
					fmt.Printf("  aliases:   %s\n", strings.Join(e.Aliases, ", "))
				}
				if e.Hook != "" {
					fmt.Println("  unload hook: yes")
				}
			}
			a.printGitStatus()
			return nil
		}),
	}
}

// printVars prints var changes as +added, ~changed, -removed.
func printVars(vars []state.VarChange) {
	for _, v := range vars {
		switch {
		case v.Old == nil:
			fmt.Printf("  + %s=%s\n", v.Key, display(v.Key, *v.New))
		case v.New == nil:
			fmt.Printf("  - %s\n", v.Key)
		case state.ListLike(*v.Old, *v.New) && !secretKey.MatchString(v.Key):
			fmt.Printf("  ~ %s  +%s\n", v.Key, strings.Join(state.AddedEntries(*v.Old, *v.New), " +"))
		default:
			fmt.Printf("  ~ %s=%s  (was %s)\n", v.Key, display(v.Key, *v.New), display(v.Key, *v.Old))
		}
	}
}

// secretKey matches var names whose values shouldn't be shown on screen.
var secretKey = regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL)`)

// display formats a value for status/diff, masking likely secrets. Long,
// key-shaped values keep a short prefix so you can tell keys apart
// (sk-ant-api03-… shows as sk-ant-****); anything shorter, like a password,
// is hidden entirely.
func display(key, value string) string {
	if !secretKey.MatchString(key) {
		return short(value)
	}
	if r := []rune(value); len(r) >= 20 {
		return string(r[:7]) + "****"
	}
	return "****"
}

func short(s string) string {
	r := []rune(strings.ReplaceAll(s, "\n", `\n`))
	if len(r) > 60 {
		return string(r[:57]) + "..."
	}
	return string(r)
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Aliases:           []string{"cat"},
		Short:             "Print a profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			p, err := a.st.Get(args[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p.Path)
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(b)
			return err
		}),
	}
}

func diffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <name>",
		Short: "Preview which env vars loading a profile would change",
		Long: `Sources the profile in a throwaway shell and compares its environment
with this terminal's. Commands in the profile really run, so side effects
(like starting an agent) still happen.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			p, err := a.st.Get(args[0])
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			argv := a.sh.Command(p.Path, exe, "__dumpenv")
			c := exec.Command(argv[0], argv[1:]...)
			c.Stderr = os.Stderr
			out, err := c.Output()
			if err != nil {
				return fmt.Errorf("sourcing %s in %s: %w", p.Path, argv[0], err)
			}
			after, err := state.DecodeSnapshot(string(out))
			if err != nil {
				return fmt.Errorf("reading env from %s: %w", argv[0], err)
			}
			e := state.Diff(p.Name, p.Path, "", &state.Snapshot{Env: trackedEnv()}, after)
			if s, err := a.state(); err == nil && s.Loaded(p.Name) {
				fmt.Printf("(%s is loaded here, so this only shows what would change on a reload)\n", p.Name)
			}
			if len(e.Vars) == 0 {
				fmt.Println("No env var changes.")
				return nil
			}
			printVars(e.Vars)
			return nil
		}),
	}
}

// Styles for list and status. color turns them off when stdout isn't a
// terminal, NO_COLOR is set or TERM is "dumb".
var (
	green = color.New(color.FgGreen).SprintFunc()
	dim   = color.New(color.Faint).SprintFunc()
)
