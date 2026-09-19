package store

import (
	"os"
	"strings"
	"testing"
)

func TestCopyOntoItselfKeepsContent(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Ext: ".sh"}
	const content = "export X=1\n"
	if _, err := s.Create("work", content); err != nil {
		t.Fatal(err)
	}
	names := []string{"work"}
	// On a case-insensitive filesystem "Work" is the same file too.
	if s.Exists("Work") {
		names = append(names, "Work")
	}
	for _, from := range names {
		if err := s.Copy(from, "work", true); err == nil || !strings.Contains(err.Error(), "same profile") {
			t.Errorf("Copy(%q, work) error = %v, want same profile error", from, err)
		}
		b, err := os.ReadFile(s.Path("work"))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != content {
			t.Fatalf("after Copy(%q, work): content = %q, want %q", from, b, content)
		}
	}
}

func TestCopy(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Ext: ".sh"}
	if _, err := s.Create("work", "export X=1\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy("work", "home", false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.Path("home")); string(b) != "export X=1\n" {
		t.Errorf("copy content = %q", b)
	}
	if err := s.Copy("work", "home", false); err == nil {
		t.Error("Copy onto an existing profile without force should fail")
	}
}
