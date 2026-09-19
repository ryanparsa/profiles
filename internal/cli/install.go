package cli

import (
	"bytes"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ryanparsa/profiles/internal/config"
	"github.com/ryanparsa/profiles/internal/shell"
	"github.com/ryanparsa/profiles/internal/store"
)

func installCmd() *cobra.Command {
	var noCompletion, noAutoload bool
	cmd := &cobra.Command{
		Use:   "install <shell>",
		Short: "Print the shell integration for your rc file",
		Long: `Print the code that defines the "profiles" shell function. Add it to your
shell's rc file so "profiles load" can change the current terminal:

  zsh   (~/.zshrc)     eval "$(profiles install zsh)"
  bash  (~/.bashrc)    eval "$(profiles install bash)"
  pwsh  ($PROFILE)     Invoke-Expression (& profiles install pwsh | Out-String)

In zsh, put the line after compinit (oh-my-zsh runs it for you) to get tab
completion.`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: shell.Names,
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, err := shell.Get(args[0])
			if err != nil {
				return err
			}
			var completion string
			if !noCompletion {
				var buf bytes.Buffer
				root := cmd.Root()
				switch sh.Name() {
				case "zsh":
					err = root.GenZshCompletion(&buf)
				case "bash":
					err = root.GenBashCompletionV2(&buf, true)
				case "pwsh":
					err = root.GenPowerShellCompletionWithDesc(&buf)
				}
				if err != nil {
					return err
				}
				completion = buf.String()
			}
			fmt.Fprint(cmd.OutOrStdout(), sh.Init(completion, !noAutoload))
			return nil
		},
	}
	cmd.Flags().BoolVar(&noCompletion, "no-completion", false, "leave out tab completion")
	cmd.Flags().BoolVar(&noAutoload, "no-autoload", false, "don't load the config's autoload profiles")
	return cmd
}

func configCmd() *cobra.Command {
	var printPath bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Edit the config file (~/.profiles/config.toml)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := store.Dir()
			if err != nil {
				return err
			}
			if printPath {
				fmt.Println(config.Path(dir))
				return nil
			}
			a, err := newApp()
			if err != nil {
				return err
			}
			path, err := config.EnsureFile(dir)
			if err != nil {
				return err
			}
			return a.openEditor(path)
		},
	}
	cmd.Flags().BoolVar(&printPath, "path", false, "print the config file path")
	return cmd
}
