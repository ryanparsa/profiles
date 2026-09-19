package shell

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

var tricky = []string{
	"plain",
	"with space",
	`it's`,
	`"double" and 'single'`,
	"$HOME `whoami` $(id) \\ back",
	"line1\nline2",
	"",
	"‘curly’ quotes",
}

// TestQuoteRoundTrip passes each value through a real shell and checks it
// comes back unchanged.
func TestQuoteRoundTrip(t *testing.T) {
	cases := []struct {
		bin   string
		argv  func(v string) []string
		quote func(string) string
	}{
		{"bash", func(v string) []string { return []string{"bash", "--norc", "-c", "printf %s " + quote(v)} }, quote},
		{"zsh", func(v string) []string { return []string{"zsh", "-f", "-c", "printf %s " + quote(v)} }, quote},
		{"pwsh", func(v string) []string {
			return []string{"pwsh", "-NoProfile", "-Command", "[Console]::OutputEncoding = [Text.Encoding]::UTF8; [Console]::Out.Write(" + pquote(v) + ")"}
		}, pquote},
	}
	for _, c := range cases {
		t.Run(c.bin, func(t *testing.T) {
			if runtime.GOOS == "windows" && c.bin != "pwsh" {
				t.Skip("Windows uses PowerShell")
			}
			if _, err := exec.LookPath(c.bin); err != nil {
				t.Skip(c.bin + " not installed")
			}
			for _, v := range tricky {
				argv := c.argv(v)
				out, err := exec.Command(argv[0], argv[1:]...).Output()
				if err != nil {
					t.Fatalf("%q: %v", v, err)
				}
				if string(out) != v {
					t.Errorf("round trip %q = %q", v, out)
				}
			}
		})
	}
}

func TestHookName(t *testing.T) {
	if got := HookName("aws-prod.eu_1"); got != "__profile_unload_aws_dprod_peu__1" {
		t.Errorf("HookName = %q", got)
	}
	// Distinct profiles must never share a hook.
	seen := map[string]string{}
	for _, name := range []string{"a-b", "a_b", "a.b", "a_db", "a__b", "a_-b", "a-_b", "a_pb", "ab"} {
		h := HookName(name)
		if other, ok := seen[h]; ok {
			t.Errorf("HookName(%q) = HookName(%q) = %q", name, other, h)
		}
		seen[h] = name
	}
}

func TestGet(t *testing.T) {
	for _, n := range []string{"zsh", "bash", "pwsh", "PowerShell"} {
		if _, err := Get(n); err != nil {
			t.Errorf("Get(%q): %v", n, err)
		}
	}
	if _, err := Get("fish"); err == nil {
		t.Error("Get(fish) should fail")
	}
}

func TestTemplateUsesName(t *testing.T) {
	for _, n := range Names {
		sh, _ := Get(n)
		if tmpl := sh.Template("work"); !strings.HasPrefix(tmpl, "# work: ") || strings.Contains(tmpl, "%!") {
			t.Errorf("%s template:\n%s", n, tmpl)
		}
	}
}
