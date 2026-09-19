package state

import (
	"maps"
	"os"
	"slices"
	"strings"
)

// Diff builds the Entry for a profile from the shell before and after it was
// sourced. hook is the name the profile's unload function was renamed to.
func Diff(name, file, hook string, before, after *Snapshot) Entry {
	e := Entry{Name: name, File: file}

	keys := slices.Collect(maps.Keys(after.Env))
	for k := range before.Env {
		if _, ok := after.Env[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)

	for _, k := range keys {
		if !Tracked(k) {
			continue
		}
		oldV, hadOld := before.Env[k]
		newV, hasNew := after.Env[k]
		switch {
		case hadOld && hasNew && oldV == newV:
		case !hadOld:
			e.Vars = append(e.Vars, VarChange{Key: k, New: &newV})
		case !hasNew:
			e.Vars = append(e.Vars, VarChange{Key: k, Old: &oldV})
		default:
			e.Vars = append(e.Vars, VarChange{Key: k, Old: &oldV, New: &newV})
		}
	}

	e.Funcs = added(before.Funcs, after.Funcs)
	e.Aliases = added(before.Aliases, after.Aliases)
	if hook != "" && slices.Contains(e.Funcs, hook) {
		e.Hook = hook
		e.Funcs = slices.DeleteFunc(e.Funcs, func(f string) bool { return f == hook })
	}
	return e
}

func added(before, after []string) []string {
	seen := map[string]bool{}
	for _, n := range before {
		seen[n] = true
	}
	var out []string
	for _, n := range after {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// ListLike reports whether a change looks like entries were added to a
// separator-delimited list such as PATH: the before value is still inside
// the after one as whole entries.
func ListLike(before, after string) bool {
	sep := string(os.PathListSeparator)
	return before != "" && after != before &&
		(strings.HasPrefix(after, before+sep) || strings.HasSuffix(after, sep+before) || strings.Contains(after, sep+before+sep))
}

// AddedEntries returns the list entries present in after but not in before.
func AddedEntries(before, after string) []string {
	counts := map[string]int{}
	for _, p := range splitList(before) {
		counts[p]++
	}
	var out []string
	for _, p := range splitList(after) {
		if counts[p] > 0 {
			counts[p]--
			continue
		}
		out = append(out, p)
	}
	return out
}

func splitList(v string) []string {
	return strings.Split(v, string(os.PathListSeparator))
}

func joinList(parts []string) string {
	return strings.Join(parts, string(os.PathListSeparator))
}
