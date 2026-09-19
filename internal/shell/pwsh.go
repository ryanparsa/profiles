package shell

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ryanparsa/profiles/internal/state"
)

// pwsh is PowerShell (7+ or Windows PowerShell 5.1).
//
// The wrapper dot-sources the generated code inside its own function scope,
// so env vars ($env:) apply to the whole session but profiles must define
// functions and aliases with global scope (function global:x, Set-Alias
// -Scope Global).
type pwsh struct{}

func (pwsh) Name() string { return "pwsh" }
func (pwsh) Ext() string  { return ".ps1" }

// exe resolves the profile binary, skipping the wrapper function.
const pwshExe = `$__pexe = (Get-Command -Name profiles -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source`

func (pwsh) Init(completion string, autoload bool) string {
	var b strings.Builder
	b.WriteString(`# profiles shell integration (pwsh)
function global:profiles {
  ` + pwshExe + `
  $__pf = Join-Path ([System.IO.Path]::GetTempPath()) ("profiles-" + [guid]::NewGuid().ToString() + ".ps1")
  $env:PROFILE_SHELL = 'pwsh'
  $env:PROFILE_EVAL_FILE = $__pf
  try { & $__pexe @args } finally { Remove-Item -LiteralPath Env:PROFILE_EVAL_FILE -ErrorAction SilentlyContinue }
  $__prc = $LASTEXITCODE
  if ((Test-Path -LiteralPath $__pf) -and (Get-Item -LiteralPath $__pf).Length -gt 0) { . $__pf }
  Remove-Item -LiteralPath $__pf -ErrorAction SilentlyContinue
  $global:LASTEXITCODE = $__prc
}
`)
	if completion != "" {
		b.WriteString(strings.TrimRight(completion, "\r\n") + "\n")
	}
	if autoload {
		b.WriteString("profiles __autoload\n")
	}
	return b.String()
}

func (pwsh) Load(name, file string, quiet bool) string {
	hook := HookName(name)
	flags := "--shell pwsh"
	if quiet {
		flags += " --quiet"
	}
	return fmt.Sprintf(`%[1]s
$__pnames = { @(Get-ChildItem Function: | ForEach-Object { 'f ' + $_.Name }) + @(Get-ChildItem Alias: | ForEach-Object { 'a ' + $_.Name }) }
$__psnap = & $__pnames | & $__pexe __snapshot
. %[2]s
if (Test-Path -LiteralPath Function:%[3]s) { Invoke-Expression ('function global:%[4]s {' + (Get-Item -LiteralPath Function:%[3]s).Definition + '}'); Remove-Item -LiteralPath Function:%[3]s }
$env:__PROFILE_SNAP = $__psnap
& $__pnames | & $__pexe __record %[6]s %[5]s %[2]s | Out-String | Invoke-Expression
Remove-Item -LiteralPath Env:__PROFILE_SNAP -ErrorAction SilentlyContinue
`, pwshExe, pquote(file), UnloadFunc, hook, pquote(name), flags)
}

func (pwsh) Op(op state.Op) string {
	switch op.Kind {
	case state.OpSetEnv:
		return fmt.Sprintf("$env:%s = %s", op.Name, pquote(op.Value))
	case state.OpUnsetEnv:
		return fmt.Sprintf("Remove-Item -LiteralPath Env:%s -ErrorAction SilentlyContinue", op.Name)
	case state.OpCallFunc:
		return fmt.Sprintf("if (Test-Path -LiteralPath Function:%[1]s) { & %[2]s }", op.Name, pquote(op.Name))
	case state.OpUnsetFunc:
		return fmt.Sprintf("Remove-Item -LiteralPath %s -ErrorAction SilentlyContinue", pquote("Function:"+op.Name))
	case state.OpUnsetAlias:
		return fmt.Sprintf("Remove-Item -LiteralPath %s -Force -ErrorAction SilentlyContinue", pquote("Alias:"+op.Name))
	}
	return ""
}

func (pwsh) SetState(encoded string) string {
	if encoded == "" {
		return "Remove-Item -LiteralPath Env:" + state.EnvState + " -ErrorAction SilentlyContinue"
	}
	return fmt.Sprintf("$env:%s = %s", state.EnvState, pquote(encoded))
}

func (pwsh) Command(file, exe string, args ...string) []string {
	bin := "pwsh"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "powershell"
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = pquote(a)
	}
	script := fmt.Sprintf(". %s *> $null; & %s %s", pquote(file), pquote(exe), strings.Join(quoted, " "))
	return []string{bin, "-NoProfile", "-NonInteractive", "-Command", script}
}

func (pwsh) Template(name string) string { return fmt.Sprintf(templatePs1, name) }

func (pwsh) RCFile() string { return "$PROFILE" }

func (pwsh) InitLine() string { return "Invoke-Expression (& profiles install pwsh | Out-String)" }

func (pwsh) EvalLine(args string) string {
	return "profiles " + args + " | Out-String | Invoke-Expression"
}

const templatePs1 = `# %[1]s: describe this profile here
#
# Dot-sourced into PowerShell by "profiles load %[1]s". Everything it adds or
# changes ($env: vars, global functions and aliases) is undone by
# "profiles unload". Define functions and aliases in global scope.

# $env:AWS_PROFILE = '%[1]s'
# $env:PATH = "$HOME\%[1]s\bin;$env:PATH"
# Set-Alias -Scope Global k kubectl
# function global:gco { git checkout @args }

# Optional: runs on unload, for cleanup profiles can't do by itself.
# function global:profile_unload {
# }
`

// pquote single-quotes s for PowerShell, which also treats the typographic
// quotes ‘ ’ ‚ ‛ as single quotes.
func pquote(s string) string {
	r := strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’",
		"‚", "‚‚", "‛", "‛‛")
	return "'" + r.Replace(s) + "'"
}
