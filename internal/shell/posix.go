package shell

import (
	"fmt"
	"strings"

	"github.com/ryanparsa/profiles/internal/state"
)

// posix covers zsh and bash, which share almost all syntax. It must stay
// compatible with bash 3.2, the version macOS ships.
type posix struct{ zsh bool }

func (p posix) Name() string {
	if p.zsh {
		return "zsh"
	}
	return "bash"
}

func (posix) Ext() string { return ".sh" }

func (p posix) Init(completion string, autoload bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `# profiles shell integration (%[1]s)
profiles() {
  local __profile_f __profile_rc
  __profile_f="$(mktemp "${TMPDIR:-/tmp}/profiles.XXXXXX")" || return 1
  PROFILE_SHELL=%[1]s PROFILE_EVAL_FILE="$__profile_f" command profiles "$@"
  __profile_rc=$?
  if [ -s "$__profile_f" ]; then . "$__profile_f"; fi
  command rm -f "$__profile_f"
  return $__profile_rc
}
`, p.Name())
	if completion != "" {
		// Completion needs compinit (zsh) / the complete builtin (bash).
		guard := "type complete >/dev/null 2>&1"
		if p.zsh {
			guard = "(( ${+functions[compdef]} ))"
		}
		fmt.Fprintf(&b, "if %s; then\n%s\nfi\n", guard, strings.TrimRight(completion, "\n"))
	}
	if autoload {
		b.WriteString("profiles __autoload\n")
	}
	return b.String()
}

// names prints "f <function>" and "a <alias>" lines for the current shell.
func (p posix) names() string {
	if p.zsh {
		return `{ printf 'f %s\n' ${(k)functions}; printf 'a %s\n' ${(k)aliases}; }`
	}
	return `{ compgen -A function | command sed 's/^/f /'; compgen -a | command sed 's/^/a /'; }`
}

func (p posix) Load(name, file string, quiet bool) string {
	hook := HookName(name)
	flags := "--shell " + p.Name()
	if quiet {
		flags += " --quiet"
	}
	var rename string
	if p.zsh {
		rename = fmt.Sprintf(`if (( ${+functions[%[1]s]} )); then eval "%[2]s() { ${functions[%[1]s]} }"; unfunction %[1]s; fi`,
			UnloadFunc, hook)
	} else {
		rename = fmt.Sprintf(`if declare -F %[1]s >/dev/null; then __profile_def="$(declare -f %[1]s)"; eval "%[2]s${__profile_def#%[1]s}"; unset -f %[1]s; unset __profile_def; fi`,
			UnloadFunc, hook)
	}
	return fmt.Sprintf(`__profile_snap="$(%[1]s | command profiles __snapshot)" && {
  . %[2]s
  %[3]s
  eval "$(%[1]s | __PROFILE_SNAP="$__profile_snap" command profiles __record %[4]s %[5]s %[2]s)"
}
unset __profile_snap
`, p.names(), quote(file), rename, flags, quote(name))
}

func (p posix) Op(op state.Op) string {
	switch op.Kind {
	case state.OpSetEnv:
		return fmt.Sprintf("export %s=%s", op.Name, quote(op.Value))
	case state.OpUnsetEnv:
		return "unset " + op.Name
	case state.OpCallFunc:
		if p.zsh {
			return fmt.Sprintf("if (( ${+functions[%[1]s]} )); then %[1]s; fi", op.Name)
		}
		return fmt.Sprintf("if declare -F %[1]s >/dev/null; then %[1]s; fi", op.Name)
	case state.OpUnsetFunc:
		return fmt.Sprintf("unset -f %s 2>/dev/null", quote(op.Name))
	case state.OpUnsetAlias:
		return fmt.Sprintf("unalias %s 2>/dev/null", quote(op.Name))
	}
	return ""
}

func (posix) SetState(encoded string) string {
	if encoded == "" {
		return "unset " + state.EnvState
	}
	return fmt.Sprintf("export %s=%s", state.EnvState, quote(encoded))
}

func (p posix) Command(file, exe string, args ...string) []string {
	script := `. "$1" >/dev/null 2>&1 </dev/null; shift; exec "$@"`
	argv := []string{"bash", "--noprofile", "--norc", "-c", script, "profiles", file, exe}
	if p.zsh {
		argv = []string{"zsh", "-f", "-c", script, "profiles", file, exe}
	}
	return append(argv, args...)
}

func (p posix) Template(name string) string { return fmt.Sprintf(templateSh, name) }

func (p posix) RCFile() string {
	if p.zsh {
		return "~/.zshrc"
	}
	return "~/.bashrc"
}

func (p posix) InitLine() string { return fmt.Sprintf(`eval "$(profiles install %s)"`, p.Name()) }

func (posix) EvalLine(args string) string { return fmt.Sprintf(`eval "$(profiles %s)"`, args) }

const templateSh = `# %[1]s: describe this profile here
#
# Sourced into your shell by "profiles load %[1]s". Everything it adds or
# changes (exported vars, functions, aliases) is undone by "profiles unload".
# Use "export", not declare/typeset/local, so variables reach your shell.

# export AWS_PROFILE=%[1]s
# export PATH="$HOME/%[1]s/bin:$PATH"
# alias k=kubectl
# gco() { git checkout "$@"; }

# Optional: runs on unload, for cleanup profiles can't do by itself.
# profile_unload() {
#   ssh-agent -k >/dev/null
# }
`

// quote single-quotes s for sh-family shells.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
