# Development

This file covers building, testing and changing `profiles`. For how to use it,
see the [README](README.md).

## Prerequisites

- Go 1.27+ (the exact version is in `go.mod`)
- git
- To run all integration tests: zsh, bash, and [PowerShell 7](https://github.com/PowerShell/PowerShell)
  (`pwsh`). Tests for a shell that isn't installed are skipped.

## Build and run

```sh
go build -o bin/profiles ./cmd/profiles      # build into ./bin (git-ignored)
go run ./cmd/profiles list                  # run without building; note the ./
go install ./cmd/profiles      # install your working copy
```

`go run cmd/profiles` (without `./`) fails with *"package cmd/profiles is not in
std"*, because Go only treats paths starting with `./` as local directories.

Neither `go run` nor `./bin/profiles` can change your current terminal. Only the
`profiles` shell function from `eval "$(profiles install zsh)"` can do that, and
it calls whichever `profiles` binary is first on your `PATH`. To try changes in
a real shell, reinstall and start a fresh shell:

```sh
go install ./cmd/profiles && exec zsh
```

To experiment without touching your real profiles, point `PROFILES_DIR`
somewhere else:

```sh
export PROFILES_DIR=$(mktemp -d)
profiles new test
```

## Tests

```sh
go test ./...                    # everything
go test ./internal/state/        # restore logic only (fast, no shells)
go test -run PowerShell -v ./cmd/profiles/
go vet ./... && gofmt -l .       # what CI checks, besides the tests
```

| Package | What's tested |
|---|---|
| `internal/state` | Diffing snapshots and restoring, including stacked `PATH` edits and vars changed after load |
| `internal/shell` | Quoting round-trips through real bash, zsh and pwsh; hook names; templates |
| `internal/store` | Copying, including onto itself |
| `internal/config` | Config file, `PROFILES_*` env overrides, invalid values |
| `internal/gitsync` | Parsing `git status` into ahead/behind/dirty |
| `internal/cli` | Secret masking, editor selection |
| `cmd/profiles` | Integration: builds the binary and drives it through real shells (load, stack, unload, hooks, autoload, git sync) |

The integration tests build the binary once in `TestMain`, put it on `PATH`,
and run scripts with a clean environment and a temporary `PROFILES_DIR`. The
posix shells run as `zsh -f` / `bash --norc --noprofile`, so your own rc files
never affect results.

CI (`.github/workflows/ci.yml`) runs `go vet`, `go test` (with `-race` on
macOS and Linux) and `go build` on Ubuntu, macOS and Windows, plus a `gofmt`
check on Ubuntu. Ubuntu installs zsh first. Windows skips the
zsh/bash tests and covers PowerShell.

## Architecture

```
cmd/profiles/        main package + integration tests
internal/cli/       cobra commands (root.go, load.go, inspect.go, files.go, install.go, git.go)
internal/shell/     code generation: posix.go (zsh + bash), pwsh.go
internal/state/     Snapshot / Entry types, Diff, Restore; pure logic, no I/O besides env
internal/store/     profile files: naming rules, list/create/copy/rename, 0600 perms
internal/config/    config.toml (PROFILES_* env overrides)
internal/emit/      hands generated shell code back to the calling shell
internal/gitsync/   thin wrapper around the system git
```

### The core problem

A child process can't modify its parent shell's environment. So the binary
never changes anything itself. It **prints shell code**, and the shell runs
that code.

### The eval-file handoff

`profiles install <shell>` prints a wrapper function (see `Init` in
`internal/shell/posix.go` and `pwsh.go`). It works in three steps:

1. The wrapper creates a temp file and runs the binary with
   `PROFILE_EVAL_FILE=<tmpfile>` and `PROFILE_SHELL=<shell>`.
2. Commands that change the shell (`load`, `unload`, `reload`)
   write code to that file with `emit.Script.Flush`. stdout and stderr stay
   connected to the terminal, which is how `list` and the editor work
   normally.
3. The wrapper sources the file and deletes it.

Without the wrapper, `PROFILE_EVAL_FILE` is unset and the code goes to stdout
for `eval "$(profiles load x)"`. If stdout is a terminal, that code would only
be printed and never run, so the binary returns `emit.ErrNoHook` and prints
setup instructions instead.

### Load: snapshot → source → record

`Shell.Load` generates code that:

1. pipes the shell's function and alias names into `profiles __snapshot`,
   which adds the exported env and prints an encoded `state.Snapshot`
2. sources the profile file
3. renames `profile_unload`, if defined, to `__profile_unload_<name>` so
   stacked profiles don't overwrite each other's hooks
4. pipes the names again into `profiles __record <name> <file>`. This takes
   the "before" snapshot from `__PROFILE_SNAP`, diffs it with the current
   state (`state.Diff`), and prints `export __PROFILE_STATE=…`.

### State

`__PROFILE_STATE` is base64 JSON of `state.State`: an ordered list of
`Entry` values, one per loaded profile, holding its added, changed and removed
vars (with old and new values), functions, aliases and hook name. Keeping it in
the environment makes it per terminal, lets subshells inherit it, and leaves
nothing on disk to go stale. To inspect it:

```sh
profiles status
echo "$__PROFILE_STATE" | base64 -d      # raw JSON (unpadded base64)
```

### Unload

`state.Restore` turns an `Entry` into a list of `state.Op` values, and each
shell renders them with `Shell.Op`. It checks the current env, which the binary
sees because it inherits the shell's environment:

| Var was… | Current value | Action |
|---|---|---|
| added | unchanged | unset |
| changed | unchanged | restore old value |
| changed, list-like (`PATH`) | other entries added since | remove only this profile's entries |
| removed | still unset | restore |
| any | changed by someone else | leave it and warn |

When several profiles are unloaded at once, one env map is updated as each is
processed, so each step sees the previous ones' results.

## Rules to keep in mind

- **stdout is for shell code.** Anything meant for the user goes to stderr
  (`warn`, `app.info`), except in plain output commands like `list`, `status`
  and `show`. `gitsync.Repo.Run` sends git's stdout to stderr for the same
  reason.
- **bash 3.2 compatibility.** macOS ships bash 3.2: no `mapfile`, no
  associative arrays, no `${var,,}`. The generated code is tested against
  `/bin/bash` on macOS.
- **Quote everything.** Use `quote` (posix) / `pquote` (PowerShell) for every
  value placed in generated code. `pquote` also doubles typographic quotes,
  because PowerShell treats `‘ ’` as quote characters.
- **Keep startup cheap.** `__snapshot` and `__record` run twice per load, so
  keep dependencies with costly package `init` out of the binary. The
  Bubble Tea picker was removed partly because lipgloss pulled in
  go-runewidth, whose `init()` took ~15 ms on every run.
- **PowerShell scope.** The wrapper dot-sources inside its own function scope.
  `$env:` changes are process-wide, but functions and aliases must be
  `global:`.

## Adding a shell

1. Implement `shell.Shell` in `internal/shell/<name>.go`: wrapper (`Init`),
   `Load`, `Op`, `SetState`, `Command` (used by `diff`), `Ext`, the new
   profile `Template`, and the setup hints `RCFile`, `InitLine`, `EvalLine`.
2. Register it in `shell.Get` and `shell.Names`.
3. Add its completion generator in `internal/cli/install.go`.
4. Add a quote round-trip case in `internal/shell/shell_test.go` and an
   integration test in `cmd/profiles/main_test.go`.
5. Install it in CI if the runners don't have it.

## Adding a command

Commands are plain cobra constructors (`fooCmd() *cobra.Command`) registered
in `newRoot` in `internal/cli/root.go`. Their names automatically become
reserved profile names (`reserved`), because `profiles <name>` would otherwise
be ambiguous. A command that changes the shell should build an `emit.Script`
and `Flush` it rather than print code.

## Debugging

```sh
profiles install zsh --no-completion           # read the wrapper
command profiles load --shell zsh work | cat   # print the generated load code without running it
command profiles unload --shell zsh | cat      # same for unload (uses this terminal's state)
PROFILES_QUIET=true profiles work              # silence ✓ messages
```

## Releasing

Releases are built by [GoReleaser](https://goreleaser.com) from
`.goreleaser.yaml`. Push a version tag and `.github/workflows/release.yml`
takes care of the rest:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The workflow runs the tests, then publishes a GitHub release for the tag with:

- `profile_<os>_<arch>.tar.gz` (`.zip` on Windows) for darwin, linux
  and windows on amd64 and arm64. Each archive contains `profiles`, `LICENSE`
  and `README.md`.
- `checksums.txt` (SHA-256)
- a changelog built from commit messages. Commits starting with `docs:`,
  `test:` or `chore:` are left out.

To check the config or build every archive locally without publishing
anything (output goes to `dist/`, which is git-ignored):

```sh
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

## Versioning

`profiles --version` reports the version stamped in at build time. Release
builds get the tag (e.g. `v0.1.0`). `go install …@v0.1.0` uses Go's build
info, and a plain local build says `dev`. To stamp a version by hand:

```sh
go build -ldflags "-X github.com/ryanparsa/profiles/internal/cli.Version=v1.2.3" ./cmd/profiles
```
