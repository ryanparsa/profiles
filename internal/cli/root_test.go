package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestEditorArgv(t *testing.T) {
	def := "vi"
	if runtime.GOOS == "windows" {
		def = "notepad"
	}
	cases := []struct {
		configured, visual, editor string
		want                       []string
	}{
		{"nvim", "code", "nano", []string{"nvim"}},
		{"", "code --wait", "nano", []string{"code", "--wait"}},
		{"", "", "nano", []string{"nano"}},
		{"  ", " ", "\t", []string{def}}, // blank values count as unset
		{"", "", "", []string{def}},
	}
	for _, c := range cases {
		t.Setenv("VISUAL", c.visual)
		t.Setenv("EDITOR", c.editor)
		if got := editorArgv(c.configured); !slices.Equal(got, c.want) {
			t.Errorf("editorArgv(%q) with VISUAL=%q EDITOR=%q = %q, want %q",
				c.configured, c.visual, c.editor, got, c.want)
		}
	}
}

func TestEditorArgvPathWithSpaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs an executable shell script")
	}
	exe := filepath.Join(t.TempDir(), "my editor")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := editorArgv(exe); !slices.Equal(got, []string{exe}) {
		t.Errorf("editorArgv(%q) = %q", exe, got)
	}
}
