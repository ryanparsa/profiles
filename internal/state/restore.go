package state

import (
	"fmt"
	"slices"
)

// OpKind is a shell operation needed to undo a profile.
type OpKind int

const (
	OpSetEnv OpKind = iota
	OpUnsetEnv
	OpCallFunc
	OpUnsetFunc
	OpUnsetAlias
)

// Op is one shell operation. Value is only used by OpSetEnv.
type Op struct {
	Kind  OpKind
	Name  string
	Value string
}

// Restore computes the operations that undo e, given the terminal's current
// env. env is updated in place so several entries can be undone in sequence.
// Vars changed by something else since the profiles loaded are left alone and
// reported as warnings.
func Restore(e *Entry, env map[string]string) (ops []Op, warnings []string) {
	if e.Hook != "" {
		ops = append(ops, Op{Kind: OpCallFunc, Name: e.Hook}, Op{Kind: OpUnsetFunc, Name: e.Hook})
	}

	set := func(k, v string) {
		ops = append(ops, Op{Kind: OpSetEnv, Name: k, Value: v})
		env[k] = v
	}
	unset := func(k string) {
		ops = append(ops, Op{Kind: OpUnsetEnv, Name: k})
		delete(env, k)
	}
	leave := func(k, why string) {
		warnings = append(warnings, fmt.Sprintf("%s: left %s alone (%s)", e.Name, k, why))
	}

	for _, v := range e.Vars {
		cur, exists := env[v.Key]
		switch {
		case v.Old == nil: // added
			switch {
			case !exists:
			case cur == *v.New:
				unset(v.Key)
			case ListLike(*v.New, cur):
				if rest := removeEntries(cur, splitList(*v.New)); rest == "" {
					unset(v.Key)
				} else {
					set(v.Key, rest)
				}
			default:
				leave(v.Key, "changed since load")
			}
		case v.New == nil: // removed
			if exists {
				leave(v.Key, "set again since load")
			} else {
				set(v.Key, *v.Old)
			}
		default: // changed
			var added []string // list entries the profile added, e.g. to PATH
			if ListLike(*v.Old, *v.New) {
				added = AddedEntries(*v.Old, *v.New)
			}
			switch {
			case !exists:
				leave(v.Key, "unset since load")
			case cur == *v.New:
				set(v.Key, *v.Old)
			case len(added) > 0 && containsAll(cur, added):
				set(v.Key, removeEntries(cur, added))
			default:
				leave(v.Key, "changed since load")
			}
		}
	}

	for _, f := range e.Funcs {
		ops = append(ops, Op{Kind: OpUnsetFunc, Name: f})
	}
	for _, a := range e.Aliases {
		ops = append(ops, Op{Kind: OpUnsetAlias, Name: a})
	}
	return ops, warnings
}

// removeEntries drops the first occurrence of each entry from a list value.
func removeEntries(list string, entries []string) string {
	parts := splitList(list)
	for _, rm := range entries {
		if i := slices.Index(parts, rm); i >= 0 {
			parts = slices.Delete(parts, i, i+1)
		}
	}
	return joinList(parts)
}

func containsAll(list string, entries []string) bool {
	parts := splitList(list)
	for _, e := range entries {
		if !slices.Contains(parts, e) {
			return false
		}
	}
	return true
}
