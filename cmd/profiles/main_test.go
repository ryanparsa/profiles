package main

// Integration tests: build the real binary and drive it through real shells.
// A shell that isn't installed is skipped.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var binDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "profiles-bin")
	if err != nil {
		panic(err)
	}
	exe := filepath.Join(dir, "profiles")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		panic("go build: " + err.Error() + "\n" + string(out))
	}
	binDir = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// env returns a clean environment with the test binary on PATH.
func env(profilesDir string, extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "PATH", "__PROFILE_STATE", "PROFILE_SHELL", "PROFILE_EVAL_FILE", "PROFILES_DIR":
			continue
		}
		if strings.HasPrefix(strings.ToUpper(k), "PROFILES_") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PROFILES_DIR="+profilesDir,
		"BASE=base",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	return append(out, extra...)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func run(t *testing.T, e []string, argv ...string) string {
	t.Helper()
	if argv[0] == "profiles" { // exec resolves names with our PATH, not e's
		argv[0] = filepath.Join(binDir, "profiles")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = e
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", argv[0], err, out)
	}
	// A shell script carries on after the binary crashes, so look for it.
	if strings.Contains(string(out), "panic: ") {
		t.Fatalf("%s: binary panicked:\n%s", argv[0], out)
	}
	return string(out)
}

// assertInOrder checks that each want line appears in out, in order.
func assertInOrder(t *testing.T, out string, want ...string) {
	t.Helper()
	rest := out
	for _, w := range want {
		i := strings.Index(rest, w)
		if i < 0 {
			t.Fatalf("missing %q (in order) in output:\n%s", w, out)
		}
		rest = rest[i+len(w):]
	}
}

const profileA = `# profiles a
export A_VAR=a
export PATH="/opt/a/bin:$PATH"
export BASE=a-base
afn() { echo afn; }
alias aal='echo aal'
profile_unload() { echo "a-hook ran, A_VAR=$A_VAR"; }
`

const profileB = `export B_VAR="it's b"
export PATH="/opt/b/bin:$PATH"
bfn() { echo bfn; }
`

const posixScript = `
eval "$(profiles install %SHELL%)"
ORIG="$PATH"
has() { if type "$1" >/dev/null 2>&1; then echo "$1=yes"; else echo "$1=no"; fi; }
hasalias() { if alias "$1" >/dev/null 2>&1; then echo "$1=yes"; else echo "$1=no"; fi; }
profiles a b
echo "1: A=$A_VAR B=$B_VAR BASE=$BASE"
has afn; has bfn; hasalias aal
profiles unload a
echo "2: A=${A_VAR-unset} B=$B_VAR BASE=$BASE"
case "$PATH" in "/opt/b/bin:$ORIG") echo "2: path ok";; *) echo "2: path bad: $PATH";; esac
has afn; has bfn; hasalias aal
profiles unload
echo "3: B=${B_VAR-unset} STATE=${__PROFILE_STATE-unset}"
if [ "$PATH" = "$ORIG" ]; then echo "3: path ok"; else echo "3: path bad: $PATH"; fi
has bfn
eval "$(command profiles load --shell %SHELL% b)"
echo "4: B=$B_VAR"
profiles a a
profiles unload a a
profiles unload
echo "5: A=${A_VAR-unset} B=${B_VAR-unset}"
if [ "$PATH" = "$ORIG" ]; then echo "5: path ok"; else echo "5: path bad: $PATH"; fi
`

func TestPosixShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses PowerShell")
	}
	shells := map[string][]string{
		"bash": {"bash", "--noprofile", "--norc", "-c"},
		"zsh":  {"zsh", "-f", "-c"},
	}
	for name, argv := range shells {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(argv[0]); err != nil {
				t.Skip(name + " not installed")
			}
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"a.sh": profileA, "b.sh": profileB})
			script := strings.ReplaceAll(posixScript, "%SHELL%", name)
			out := run(t, env(dir), append(argv, script)...)
			assertInOrder(t, out,
				"✓ loaded a", "✓ loaded b",
				"1: A=a B=it's b BASE=a-base", "afn=yes", "bfn=yes", "aal=yes",
				"a-hook ran, A_VAR=a",
				"2: A=unset B=it's b BASE=base", "2: path ok", "afn=no", "bfn=yes", "aal=no",
				"3: B=unset STATE=unset", "3: path ok", "bfn=no",
				"4: B=it's b",
				"5: A=unset B=unset", "5: path ok",
			)
		})

		t.Run(name+"/autoload", func(t *testing.T) {
			if _, err := exec.LookPath(argv[0]); err != nil {
				t.Skip(name + " not installed")
			}
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"a.sh": profileA, "b.sh": profileB, "config.toml": "autoload = [\"b\"]\n"})
			script := `eval "$(profiles install ` + name + `)"; echo "B=$B_VAR"`
			assertInOrder(t, run(t, env(dir), append(argv, script)...), "B=it's b")

			// Env vars override the config file.
			script = `eval "$(profiles install ` + name + `)"; echo "A=$A_VAR B=${B_VAR-unset}"`
			assertInOrder(t, run(t, env(dir, "PROFILES_AUTOLOAD=a"), append(argv, script)...), "A=a B=unset")
		})
	}
}

const pwshA = `# profiles a
$env:A_VAR = 'a'
$env:PATH = '/opt/a/bin' + [IO.Path]::PathSeparator + $env:PATH
$env:BASE = 'a-base'
function global:afn { 'afn' }
Set-Alias -Scope Global aal Get-Date
function global:profile_unload { "a-hook ran, A_VAR=$env:A_VAR" }
`

const pwshB = `$env:B_VAR = "it's b"
$env:PATH = '/opt/b/bin' + [IO.Path]::PathSeparator + $env:PATH
function global:bfn { 'bfn' }
`

const pwshScript = `
$ErrorActionPreference = 'Stop'
Invoke-Expression (& profiles install pwsh | Out-String)
$orig = $env:PATH
function has($n) { if (Test-Path "Function:$n") { "$n=yes" } else { "$n=no" } }
function hasalias($n) { if (Test-Path "Alias:$n") { "$n=yes" } else { "$n=no" } }
profiles a b
"1: A=$env:A_VAR B=$env:B_VAR BASE=$env:BASE"
has afn; has bfn; hasalias aal
profiles unload a
"2: A=$(if ($null -eq $env:A_VAR) { 'unset' } else { $env:A_VAR }) B=$env:B_VAR BASE=$env:BASE"
if ($env:PATH -eq ('/opt/b/bin' + [IO.Path]::PathSeparator + $orig)) { '2: path ok' } else { "2: path bad: $env:PATH" }
has afn; has bfn; hasalias aal
profiles unload
"3: B=$(if ($null -eq $env:B_VAR) { 'unset' } else { $env:B_VAR }) STATE=$(if ($null -eq $env:__PROFILE_STATE) { 'unset' } else { 'set' })"
if ($env:PATH -eq $orig) { '3: path ok' } else { "3: path bad: $env:PATH" }
has bfn
`

func TestPowerShell(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.ps1": pwshA, "b.ps1": pwshB})
	out := run(t, env(dir), "pwsh", "-NoProfile", "-NonInteractive", "-Command", pwshScript)
	assertInOrder(t, out,
		"✓ loaded a", "✓ loaded b",
		"1: A=a B=it's b BASE=a-base", "afn=yes", "bfn=yes", "aal=yes",
		"a-hook ran, A_VAR=a",
		"2: A=unset B=it's b BASE=base", "2: path ok", "afn=no", "bfn=yes", "aal=no",
		"3: B=unset STATE=unset", "3: path ok", "bfn=no",
	)
}

// Profiles whose names differ only in "-" vs "_" each keep their own
// unload hook.
func TestHooksDontCollide(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses PowerShell")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"x-y.sh": "profile_unload() { echo 'hook of x-y'; }\n",
		"x_y.sh": "profile_unload() { echo 'hook of x_y'; }\n",
	})
	script := `eval "$(profiles install bash)"; profiles x-y x_y; profiles unload x-y; echo sep; profiles unload x_y`
	assertInOrder(t, run(t, env(dir), "bash", "--noprofile", "--norc", "-c", script),
		"hook of x-y", "sep", "hook of x_y")
}

func TestGitSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	m1, m2 := filepath.Join(root, "m1"), filepath.Join(root, "m2")
	for _, d := range []string{m1, m2} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run(t, env(root), "git", "init", "-q", "--bare", remote)
	writeFiles(t, m1, map[string]string{"work.sh": "export X=1\n"})

	assertInOrder(t, run(t, env(m1), "profiles", "link", remote), "linked", "pushed")
	assertInOrder(t, run(t, env(m2), "profiles", "link", remote), "linked")
	checkFile(t, filepath.Join(m2, "work.sh"), "export X=1\n")

	writeFiles(t, m2, map[string]string{"work.sh": "export X=2\n", "home.sh": "export Y=1\n"})
	assertInOrder(t, run(t, env(m2), "profiles", "push", "-m", "update"), "private repo", "committed and pushed")

	assertInOrder(t, run(t, env(m1), "profiles", "pull"), "pulled")
	checkFile(t, filepath.Join(m1, "work.sh"), "export X=2\n")
	checkFile(t, filepath.Join(m1, "home.sh"), "export Y=1\n")
	assertInOrder(t, run(t, env(m1), "profiles", "pull"), "already up to date")
}

func checkFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(b), "\r\n", "\n"); got != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s mode = %v, want 0600", path, info.Mode().Perm())
	}
}
