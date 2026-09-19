package config

import (
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestAutoloadFromEnv(t *testing.T) {
	cases := map[string][]string{
		"default work": {"default", "work"},
		"default,work": {"default", "work"},
		" a ,  b\tc ":  {"a", "b", "c"},
		"single":       {"single"},
	}
	for env, want := range cases {
		t.Setenv("PROFILES_AUTOLOAD", env)
		c, err := Load(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(c.Autoload, want) {
			t.Errorf("PROFILES_AUTOLOAD=%q: Autoload = %q, want %q", env, c.Autoload, want)
		}
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	const toml = "autoload = [\"a\", \"b\"]\nquiet = true\n"
	if err := os.WriteFile(Path(dir), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Autoload, []string{"a", "b"}) || !c.Quiet || !c.ConfirmDelete {
		t.Errorf("Load = %+v", c)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	const toml = "autoload = [\"a\"]\neditor = \"vi\"\nconfirm_delete = true\n"
	if err := os.WriteFile(Path(dir), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROFILES_AUTOLOAD", "b")
	t.Setenv("PROFILES_EDITOR", "code --wait")
	t.Setenv("PROFILES_CONFIRM_DELETE", "false")
	t.Setenv("PROFILES_QUIET", "1")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Autoload: []string{"b"}, Editor: "code --wait", ConfirmDelete: false, Quiet: true}
	if !reflect.DeepEqual(*c, want) {
		t.Errorf("Load = %+v, want %+v", *c, want)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Run("bad env bool", func(t *testing.T) {
		t.Setenv("PROFILES_QUIET", "maybe")
		if _, err := Load(t.TempDir()); err == nil {
			t.Error("want error for PROFILES_QUIET=maybe")
		}
	})
	t.Run("bad toml", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(Path(dir), []byte("quiet = \"yes\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Error("want error for quiet = \"yes\"")
		}
	})
}

func TestDefaultFileParses(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureFile(dir); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Config{ConfirmDelete: true}); !reflect.DeepEqual(*c, want) {
		t.Errorf("default file = %+v, want %+v", *c, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Autoload) != 0 || c.Quiet || !c.ConfirmDelete || c.Editor != "" {
		t.Errorf("defaults = %+v", c)
	}
}
