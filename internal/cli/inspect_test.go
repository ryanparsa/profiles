package cli

import "testing"

func TestDisplayMasksSecrets(t *testing.T) {
	cases := []struct{ key, value, want string }{
		{"ANTHROPIC_API_KEY", "sk-ant-api03-abcdefghijkl", "sk-ant-****"},
		{"OPENAI_API_KEY", "sk-proj-abcdefghijklmnop", "sk-proj****"},
		{"GITHUB_TOKEN", "short", "****"},
		{"db_password", "hunter22hunter", "****"}, // short secrets reveal nothing
		{"SECRET_NOTE", "ünïcödé-ünïcödé-ünïcödé", "ünïcödé****"},
		{"AWS_PROFILE", "work", "work"},
		{"KUBECONFIG", "/Users/me/.kube/work", "/Users/me/.kube/work"},
	}
	for _, c := range cases {
		if got := display(c.key, c.value); got != c.want {
			t.Errorf("display(%q, %q) = %q, want %q", c.key, c.value, got, c.want)
		}
	}
}

func TestShortCutsOnRunes(t *testing.T) {
	long := ""
	for range 70 {
		long += "é"
	}
	got := short(long)
	if want := string([]rune(long)[:57]) + "..."; got != want {
		t.Errorf("short = %q, want %q", got, want)
	}
}
