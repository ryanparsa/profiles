# profiles

[![CI](https://github.com/ryanparsa/profiles/actions/workflows/ci.yml/badge.svg)](https://github.com/ryanparsa/profiles/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ryanparsa/profiles.svg)](https://pkg.go.dev/github.com/ryanparsa/profiles)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**`profiles` loads and unloads environment profiles in your current terminal.**

Instead of one global pile of `export`s in `.zshrc`, keep one small file per
context (`work`, `personal`, `aws-prod`, …) in `~/.profiles` and switch
between them. Everything a profile adds or changes (env vars, functions,
aliases) is undone again on unload. You never write cleanup code.

```console
$ profiles work
✓ loaded work (+3 vars, ~1 var, 1 function)
$ profiles status
● work ~/.profiles/work.sh
  + AWS_PROFILE=work
  + KUBECONFIG=/Users/me/.kube/work
  + OPENAI_API_KEY=sk-proj****
  ~ PATH  +/Users/me/work/bin
  functions: gco
$ profiles unload
✓ unloaded work
```

For example, use your company's OpenAI and Anthropic keys in `work` and your
own keys in `personal`, and never paste the wrong key into the wrong terminal
again.

## Features

- **Plain shell scripts.** A profile is just a file that gets sourced, so it
  can hold env vars, functions, aliases, or any other shell code.
- **Automatic unload.** The tool records what each profile changed and puts
  it back exactly. `PATH`-style lists are handled entry by entry.
- **Stacking.** Load several profiles at once and unload them in any order.
- **Tab completion.** Profile names complete in zsh, bash and PowerShell,
  with each profile's description.
- **Per terminal.** Nothing global changes, and other terminals are
  unaffected.
- **Git sync.** `profiles link`, `push` and `pull` keep your profiles in a
  private repo across machines.
- **Cross-platform.** Works with zsh and bash on macOS and Linux, and with
  PowerShell on Windows.

## Install

Download a prebuilt binary for macOS, Linux or Windows (amd64 / arm64) from
the [latest release](https://github.com/ryanparsa/profiles/releases/latest),
and put `profiles` somewhere on your `PATH`:

```sh
# macOS on Apple Silicon: use darwin_amd64, linux_amd64, linux_arm64, … for others
curl -fsSL https://github.com/ryanparsa/profiles/releases/latest/download/profiles_darwin_arm64.tar.gz | tar xz profiles
mv profiles ~/.local/bin/
```

Or with Go 1.27+:

```sh
go install github.com/ryanparsa/profiles/cmd/profiles@latest
```

Or from source:

```sh
git clone https://github.com/ryanparsa/profiles.git
cd profiles
go build -o ~/.local/bin/profiles ./cmd/profiles   # any directory on your PATH
```

### Shell setup

Add one line to your shell's rc file, then open a new terminal:

| Shell      | File                                      | Line                                                       |
|------------|-------------------------------------------|------------------------------------------------------------|
| zsh        | `~/.zshrc` (after oh-my-zsh / `compinit`) | `eval "$(profiles install zsh)"`                            |
| bash       | `~/.bashrc`                               | `eval "$(profiles install bash)"`                           |
| PowerShell | `$PROFILE`                                | `Invoke-Expression (& profiles install pwsh \| Out-String)` |

A program can't change its parent shell's environment. So `profiles install`
prints a small `profiles` shell function that runs the binary and applies the
code it hands back. It also sets up tab completion.

Without the rc line you can still run one command at a time with
`eval "$(profiles load work)"` (PowerShell:
`profiles load work | Out-String | Invoke-Expression`).

## Quick start

```sh
profiles new work     # creates ~/.profiles/work.sh from a template and opens $EDITOR
profiles work         # load it (same as: profiles load work)
profiles status       # see what it changed
profiles unload       # undo everything
```

## Usage

```sh
profiles                  # list profiles (same as profiles list)
profiles work             # load a profile
profiles work aws-prod    # load several; they stack
profiles work --no-shared # load without the "shared" profile
profiles unload aws-prod  # unload one…
profiles unload           # …or all of them
profiles reload           # re-source loaded profiles after editing them
profiles status           # what's loaded here and what it changed
profiles list             # all profiles (● = loaded in this terminal)

profiles new <name>       # create from a template and open $EDITOR
profiles edit <name>
profiles show <name>
profiles diff <name>      # preview which env vars loading it would change
profiles cp <src> <dst>
profiles rename <old> <new>
profiles rm <name>...     # asks first (-f to skip)

profiles config           # edit ~/.profiles/config.toml
profiles install <shell>  # print the shell integration (zsh, bash, pwsh)
```

Run `profiles <command> --help` for details.

## Writing profiles

Profiles live in `~/.profiles`: `<name>.sh` for zsh and bash, `<name>.ps1`
for PowerShell.

```sh
# work: AWS, kube and AI keys for the day job   <- first comment = description in list
export AWS_PROFILE=work
export KUBECONFIG="$HOME/.kube/work"
export OPENAI_API_KEY=sk-proj-...
export PATH="$HOME/work/bin:$PATH"
alias k=kubectl
gco() { git checkout "$@"; }

# Optional: runs on unload, for cleanup profiles can't do by itself.
profile_unload() {
  ssh-agent -k >/dev/null
}
```

PowerShell equivalent:

```powershell
# work: AWS, kube and AI keys for the day job
$env:AWS_PROFILE = 'work'
$env:OPENAI_API_KEY = 'sk-proj-...'
$env:PATH = "$HOME\work\bin;$env:PATH"
Set-Alias -Scope Global k kubectl
function global:gco { git checkout @args }
function global:profile_unload { }
```

Rules of thumb:

- **Use `export`.** Only exported variables are tracked. The file is sourced
  inside a function, so `declare`, `typeset` and `local` create local
  variables. In PowerShell, use `$env:` and give functions and aliases global
  scope.
- **Changes made after loading win.** If something else changes a variable
  after the profiles loaded, unload leaves that variable alone and warns you.
- **Overwritten functions stay.** Functions the profile defines are removed on
  unload. If the profile replaced a function that already existed, that
  function isn't restored.
- **Names** use letters, digits, `.`, `_` and `-`, and can't be command names
  such as `list`.
- **Secrets are masked on screen.** For vars whose names contain `KEY`,
  `TOKEN`, `SECRET`, `PASSWORD`, `PASSWD` or `CREDENTIAL`, `profiles status`
  and `profiles diff` show only the first 7 characters of long values (so you
  can tell keys apart, e.g. `sk-ant-****`) and hide short values entirely.

## Predefined profiles

The first run (when `~/.profiles/config.toml` doesn't exist yet) creates the
config file and two profiles. After that, they're regular files: deleting one
doesn't bring it back.

- **`default`** is loaded in new terminals when `config.toml` doesn't set
  `autoload`. Set `autoload` to other profiles to load those instead, or to
  `[]` to load nothing. Otherwise it's a regular profile: `profiles default`.
- **`shared`** is for settings you want everywhere. It's loaded first
  whenever profiles are loaded, both in new terminals and with
  `profiles <name>`, so the other profiles can override it. Leave it out with
  `--no-shared` (`profiles work --no-shared`), or in new terminals with
  `eval "$(profiles install zsh --no-shared)"`.

## Configuration

`profiles config` opens `~/.profiles/config.toml`:

```toml
autoload = ["default"]   # profiles loaded in every new terminal (unset: "default")
editor = "code -w"       # default: $VISUAL, then $EDITOR, then vi / notepad
confirm_delete = true    # ask before `profiles rm`
quiet = false            # hide the ✓ messages
```

Every key can also be set from the environment with a `PROFILES_` prefix, for
example `PROFILES_QUIET=true` or `PROFILES_AUTOLOAD="default work"` (names
separated by spaces or commas). Invalid values are reported as errors. To use
a directory other than `~/.profiles`, set `PROFILES_DIR`. Colors in `list` and
`status` are turned off when `NO_COLOR` is set.

## Sync with git

```sh
profiles link git@github.com:you/my-profiles.git   # first machine: commits and pushes your profiles
profiles link git@github.com:you/my-profiles.git   # another machine: checks them out
profiles push                                       # commit everything and push
profiles pull                                       # pull --rebase, then says which loaded profiles to reload
profiles git log --oneline                          # run any git command in ~/.profiles
```

> [!WARNING]
> Profiles usually contain secrets. Only sync them to a **private**
> repository.

`profiles` uses your system `git`, so your existing SSH keys and credential
helpers work. Profiles named `*.local.sh` / `*.local.ps1` are git-ignored, so
they stay on one machine. After a pull, profile files are reset to mode
`0600`.

## How it works

1. `profiles load work` records a snapshot of the shell: its exported
   environment plus the names of its functions and aliases.
2. It sources `work.sh` and records a second snapshot.
3. The difference is stored in the `__PROFILE_STATE` environment variable.
   That keeps state per terminal, lets subshells inherit it, and means no
   state files go stale on disk.
4. `profiles unload work` turns that record back into shell code. It runs
   `profile_unload` if the profile defines one, restores or unsets variables,
   and removes the profile's functions and aliases.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for
building, testing, and how the internals fit together.

## License

Copyright 2026 Ryan Parsa

Licensed under the [Apache License, Version 2.0](LICENSE).
