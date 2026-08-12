package main

import (
	"slices"
	"testing"
)

// splitFlags exists so argument order does not change meaning. These
// cases are the ones that were wrong or would have gone wrong.
func TestSplitFlags(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		valueFlags []string
		positional []string
		flags      []string
	}{
		{
			// The bug that produced splitFlags: Go's flag package stops
			// at the first non-flag argument, so `eng clean . --yes` left
			// --yes unparsed and the command told the developer to re-run
			// with the flag they had just passed.
			name:       "a boolean flag after a path",
			args:       []string{".", "--yes"},
			positional: []string{"."},
			flags:      []string{"--yes"},
		},
		{
			name:       "the same flag before the path",
			args:       []string{"--yes", "."},
			positional: []string{"."},
			flags:      []string{"--yes"},
		},
		{
			// Without valueFlags this splits into flags=[--rules] and
			// positional=[~/os, ./engineering] — so `eng setup` would
			// create the workspace at ./engineering and attach nothing,
			// which is wrong in a way that looks like it worked.
			name:       "a flag that takes a value",
			args:       []string{"~/os", "--rules", "./engineering"},
			valueFlags: []string{"rules"},
			positional: []string{"~/os"},
			flags:      []string{"--rules", "./engineering"},
		},
		{
			name:       "the value attached with =",
			args:       []string{"~/os", "--rules=./engineering"},
			valueFlags: []string{"rules"},
			positional: []string{"~/os"},
			flags:      []string{"--rules=./engineering"},
		},
		{
			name:       "repeated value flags and a boolean between them",
			args:       []string{"~/os", "--rules", "a", "--force", "--repo", "b"},
			valueFlags: []string{"rules", "repo"},
			positional: []string{"~/os"},
			flags:      []string{"--rules", "a", "--force", "--repo", "b"},
		},
		{
			name:       "single-dash spelling",
			args:       []string{"-rules", "a", "~/os"},
			valueFlags: []string{"rules"},
			positional: []string{"~/os"},
			flags:      []string{"-rules", "a"},
		},
		{
			// A value flag at the end with nothing after it must not
			// consume past the slice; flag.Parse reports the missing
			// value itself, with a better message than this could.
			name:       "a value flag with no value",
			args:       []string{"--rules"},
			valueFlags: []string{"rules"},
			positional: nil,
			flags:      []string{"--rules"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			positional, flags := splitFlags(c.args, c.valueFlags...)
			if !slices.Equal(positional, c.positional) {
				t.Errorf("positional = %q, want %q", positional, c.positional)
			}
			if !slices.Equal(flags, c.flags) {
				t.Errorf("flags = %q, want %q", flags, c.flags)
			}
		})
	}
}

// A repeated flag must collect rather than replace: a workspace is
// usually built from several repositories, and keeping only the last one
// would lose the others without saying so.
func TestRepeatableCollects(t *testing.T) {
	var got []string
	r := repeatable{&got}
	for _, v := range []string{"a", "b", "c"} {
		if err := r.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("collected %q, want all three", got)
	}
}
