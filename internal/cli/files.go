package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCmd() *cobra.Command {
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a profile and open it in your editor",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateNewName(cmd, name); err != nil {
				return err
			}
			path, err := a.st.Create(name, a.sh.Template(name))
			if err != nil {
				return err
			}
			if !noEdit {
				if err := a.openEditor(path); err != nil {
					return fmt.Errorf("created %s, but %w; open it later with: profiles edit %s", path, err, name)
				}
			}
			a.info("✓ created %s. Load it with: profiles %s", path, name)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "don't open the editor")
	return cmd
}

func editCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "edit <name>",
		Short:             "Open a profile in your editor",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			return a.edit(args[0])
		}),
	}
}

func (a *app) edit(name string) error {
	p, err := a.st.Get(name)
	if err != nil {
		return err
	}
	if err := a.openEditor(p.Path); err != nil {
		return err
	}
	if s, err := a.state(); err == nil && s.Loaded(name) {
		a.info("%s is loaded here; apply your changes with: profiles reload %s", name, name)
	}
	return nil
}

func rmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "rm <name>...",
		Aliases:           []string{"remove", "delete"},
		Short:             "Delete profiles",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			s, _ := a.state()
			for _, name := range args {
				p, err := a.st.Get(name)
				if err != nil {
					return err
				}
				if a.cfg.ConfirmDelete && !force && !confirm(fmt.Sprintf("Delete %s?", p.Path)) {
					a.info("skipped %s", name)
					continue
				}
				if err := a.st.Remove(name); err != nil {
					return err
				}
				a.info("✓ deleted %s", name)
				if s != nil && s.Loaded(name) {
					warn("%s is still loaded here; run: profiles unload %s", name, name)
				}
			}
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "don't ask for confirmation")
	return cmd
}

// stdin is shared by every prompt: a new bufio.Reader per prompt would buffer
// answers meant for later prompts and lose them.
var stdin = bufio.NewReader(os.Stdin)

func confirm(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	answer, _ := stdin.ReadString('\n')
	if !isTerminal(os.Stdin) { // piped answers aren't echoed; end the prompt line
		fmt.Fprintln(os.Stderr)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func renameCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "rename <old> <new>",
		Aliases:           []string{"mv"},
		Short:             "Rename a profile",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			if err := validateNewName(cmd, args[1]); err != nil {
				return err
			}
			if err := a.st.Rename(args[0], args[1], force); err != nil {
				return err
			}
			a.info("✓ renamed %s to %s", args[0], args[1])
			if s, err := a.state(); err == nil && s.Loaded(args[0]) {
				warn("%s is still loaded here under its old name; unload it with: profiles unload %s", args[0], args[0])
			}
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing profile")
	return cmd
}

func cpCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "cp <src> <dst>",
		Aliases:           []string{"copy"},
		Short:             "Copy a profile",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeProfiles(false),
		RunE: withApp(func(a *app, cmd *cobra.Command, args []string) error {
			if err := validateNewName(cmd, args[1]); err != nil {
				return err
			}
			if err := a.st.Copy(args[0], args[1], force); err != nil {
				return err
			}
			a.info("✓ copied %s to %s", args[0], args[1])
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing profile")
	return cmd
}
