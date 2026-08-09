package search

import "testing"

// TestUserQueryQuotesTermsThatFTS5WouldReadAsSyntax covers the crash
// this function exists for: a hyphen makes FTS5 read `a-b` as a column
// filter, so searching for a hyphenated repository name was a SQL
// error rather than a result.
func TestUserQueryQuotesTermsThatFTS5WouldReadAsSyntax(t *testing.T) {
	for _, tt := range []struct {
		name, in, want string
	}{
		{"hyphen", "engineering-mcp kernel policy", `"engineering-mcp" "kernel" "policy"`},
		{"plain words unchanged in meaning", "kernel policy", `"kernel" "policy"`},
		{"colon", "modified: readme.md", `"modified:" "readme.md"`},
		{"embedded quote is doubled", `say "hi"`, `"say" """hi"""`},
		{"caret and star", "foo^ bar*", `"foo^" "bar*"`},
		{"collapses irregular whitespace", "  a \t b \n", `"a" "b"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := UserQuery(tt.in)
			if !ok {
				t.Fatalf("UserQuery(%q) reported nothing searchable", tt.in)
			}
			if got != tt.want {
				t.Errorf("UserQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestUserQueryPreservesImplicitAnd pins the scope of the fix: terms are
// joined with spaces, not OR. This repairs a crash; retuning what search
// considers a match is a separate decision with its own evidence.
func TestUserQueryPreservesImplicitAnd(t *testing.T) {
	if got, _ := UserQuery("alpha beta"); got != `"alpha" "beta"` {
		t.Errorf("UserQuery = %q — joining with OR would change what search returns, not just what it accepts", got)
	}
}

// TestUserQueryReportsWhenThereIsNothingToSearch: an earlier version
// returned blank input unchanged, which handed FTS5 an empty MATCH —
// `fts5: syntax error near ""`, the exact class of failure this function
// exists to remove. There is no query meaning "match nothing", so the
// caller is told instead and returns no results.
func TestUserQueryReportsWhenThereIsNothingToSearch(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		if got, ok := UserQuery(in); ok {
			t.Errorf("UserQuery(%q) = %q, ok — blank input must not reach FTS5", in, got)
		}
	}
}
