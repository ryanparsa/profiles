package gitsync

import "testing"

func TestParseStatus(t *testing.T) {
	cases := []struct {
		out  string
		want Status
		str  string
	}{
		{"## main...origin/main", Status{}, "up to date"},
		{"## main...origin/main [ahead 2]", Status{Ahead: 2}, "↑2"},
		{"## main...origin/main [ahead 1, behind 12]\n M work.sh", Status{Ahead: 1, Behind: 12, Dirty: true}, "↑1, ↓12, uncommitted changes"},
		{"## No commits yet on main\n?? work.sh", Status{Dirty: true}, "uncommitted changes"},
	}
	for _, c := range cases {
		got := parseStatus(c.out)
		if got != c.want || got.String() != c.str {
			t.Errorf("parseStatus(%q) = %+v %q, want %+v %q", c.out, got, got, c.want, c.str)
		}
	}
}
