package state

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func list(parts ...string) string { return strings.Join(parts, string(os.PathListSeparator)) }

func snap(env map[string]string, funcs ...string) *Snapshot {
	return &Snapshot{Env: env, Funcs: funcs}
}

func TestDiff(t *testing.T) {
	before := snap(map[string]string{"KEEP": "1", "EDITOR": "vi", "GONE": "x", "PWD": "/a"}, "old")
	after := snap(map[string]string{"KEEP": "1", "EDITOR": "nvim", "NEW": "n", "PWD": "/b", "BAD-KEY": "z"},
		"old", "gco", "__profile_unload_work")
	after.Aliases = []string{"k"}

	e := Diff("work", "/p/work.sh", "__profile_unload_work", before, after)

	got := map[string]string{}
	for _, v := range e.Vars {
		switch {
		case v.Old == nil:
			got[v.Key] = "+" + *v.New
		case v.New == nil:
			got[v.Key] = "-" + *v.Old
		default:
			got[v.Key] = *v.Old + ">" + *v.New
		}
	}
	want := map[string]string{"EDITOR": "vi>nvim", "NEW": "+n", "GONE": "-x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vars = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(e.Funcs, []string{"gco"}) {
		t.Errorf("funcs = %v", e.Funcs)
	}
	if !reflect.DeepEqual(e.Aliases, []string{"k"}) {
		t.Errorf("aliases = %v", e.Aliases)
	}
	if e.Hook != "__profile_unload_work" {
		t.Errorf("hook = %q", e.Hook)
	}
}

// load simulates sourcing a profile: mutate env, then diff.
func load(t *testing.T, name string, env map[string]string, mutate func(map[string]string)) Entry {
	t.Helper()
	before := map[string]string{}
	for k, v := range env {
		before[k] = v
	}
	mutate(env)
	return Diff(name, name+".sh", "", snap(before), snap(env))
}

func TestRestoreSimple(t *testing.T) {
	env := map[string]string{"EDITOR": "vi", "GONE": "x"}
	e := load(t, "a", env, func(m map[string]string) {
		m["EDITOR"] = "nvim"
		m["NEW"] = "1"
		delete(m, "GONE")
	})
	_, warns := Restore(&e, env)
	if len(warns) > 0 {
		t.Errorf("warnings: %v", warns)
	}
	want := map[string]string{"EDITOR": "vi", "GONE": "x"}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("env = %v, want %v", env, want)
	}
}

func TestRestoreStackedPath(t *testing.T) {
	orig := list("/usr/bin", "/bin")
	env := map[string]string{"PATH": orig}
	a := load(t, "a", env, func(m map[string]string) { m["PATH"] = list("/opt/a", m["PATH"]) })
	b := load(t, "b", env, func(m map[string]string) { m["PATH"] = list("/opt/b", m["PATH"], "/opt/b2") })

	// Unload a first (out of order): b's entries must survive.
	Restore(&a, env)
	if want := list("/opt/b", "/usr/bin", "/bin", "/opt/b2"); env["PATH"] != want {
		t.Fatalf("after unloading a: PATH = %q, want %q", env["PATH"], want)
	}
	Restore(&b, env)
	if env["PATH"] != orig {
		t.Fatalf("after unloading b: PATH = %q, want %q", env["PATH"], orig)
	}
}

func TestListLike(t *testing.T) {
	cases := []struct {
		old, new string
		want     bool
	}{
		{list("/a", "/b"), list("/x", "/a", "/b"), true},
		{list("/a", "/b"), list("/a", "/b", "/x"), true},
		{"/a", list("/x", "/a", "/y"), true},
		{"base", "a-base", false},
		{"", "/x", false},
		{"/a", "/a", false},
	}
	for _, c := range cases {
		if got := ListLike(c.old, c.new); got != c.want {
			t.Errorf("ListLike(%q, %q) = %v, want %v", c.old, c.new, got, c.want)
		}
	}
}

func TestRestoreModifiedSinceLoad(t *testing.T) {
	env := map[string]string{"EDITOR": "vi"}
	e := load(t, "a", env, func(m map[string]string) { m["EDITOR"] = "nvim"; m["NEW"] = "1" })
	env["EDITOR"] = "emacs"
	env["NEW"] = "2"
	ops, warns := Restore(&e, env)
	if len(warns) != 2 {
		t.Errorf("warnings = %v, want 2", warns)
	}
	if len(ops) != 0 {
		t.Errorf("ops = %v, want none", ops)
	}
	if env["EDITOR"] != "emacs" || env["NEW"] != "2" {
		t.Errorf("env changed: %v", env)
	}
}

func TestRestoreHookFirstThenFuncs(t *testing.T) {
	e := Entry{Name: "a", Hook: "__profile_unload_a", Funcs: []string{"gco"}, Aliases: []string{"k"}}
	ops, _ := Restore(&e, map[string]string{})
	want := []Op{
		{Kind: OpCallFunc, Name: "__profile_unload_a"},
		{Kind: OpUnsetFunc, Name: "__profile_unload_a"},
		{Kind: OpUnsetFunc, Name: "gco"},
		{Kind: OpUnsetAlias, Name: "k"},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %v, want %v", ops, want)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	v := "line1\nit's \"quoted\" $HOME"
	s := &State{Profiles: []Entry{{Name: "a", File: "/a.sh", Vars: []VarChange{{Key: "X", New: &v}}}}}
	t.Setenv(EnvState, s.Encode())
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("round trip = %+v, want %+v", got, s)
	}
	if (&State{}).Encode() != "" {
		t.Error("empty state should encode to empty string")
	}
}

func TestParseNames(t *testing.T) {
	f, a := ParseNames("f gco\na k\nf \n\nf  spaced \n")
	if !reflect.DeepEqual(f, []string{"gco", "spaced"}) || !reflect.DeepEqual(a, []string{"k"}) {
		t.Errorf("funcs=%v aliases=%v", f, a)
	}
}
