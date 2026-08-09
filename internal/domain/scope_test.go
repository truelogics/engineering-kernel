package domain

import "testing"

func TestParseScopeUniversalWhenEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", ",", " , "} {
		s := ParseScope(in)
		if !s.Universal() {
			t.Errorf("ParseScope(%q).Universal() = false, want true", in)
		}
		if !s.Matches([]string{"anything.go"}) {
			t.Errorf("ParseScope(%q) should match every path", in)
		}
	}
}

func TestParseScopeExpandsBareTokenToExtension(t *testing.T) {
	got := ParseScope("go").Patterns()
	if len(got) != 1 || got[0] != "**/*.go" {
		t.Fatalf("ParseScope(\"go\").Patterns() = %v, want [**/*.go]", got)
	}
}

func TestParseScopeKeepsExplicitPatterns(t *testing.T) {
	got := ParseScope("**/*.ts, **/*.tsx").Patterns()
	want := []string{"**/*.ts", "**/*.tsx"}
	if len(got) != len(want) {
		t.Fatalf("Patterns() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Patterns() = %v, want %v", got, want)
		}
	}
}

// TestScopeMatchesTheRealRulebook is the case RFC-0005 exists for: the
// first AI Review run that ever retrieved an engineering rule retrieved
// a TypeScript rule for a Go diff.
func TestScopeMatchesTheRealRulebook(t *testing.T) {
	goDiff := []string{"internal/provider/claude/claude.go", "internal/formatter/markdown/markdown.go"}

	if ParseScope("**/*.ts, **/*.tsx").Matches(goDiff) {
		t.Error("a TypeScript rule must not apply to a Go-only change")
	}
	if !ParseScope("go").Matches(goDiff) {
		t.Error("a Go rule must apply to a Go change")
	}
	if !ParseScope("**").Matches(goDiff) {
		t.Error("a ** rule must apply to everything")
	}
	if ParseScope("**/tsconfig*.json").Matches(goDiff) {
		t.Error("a tsconfig rule must not apply to a Go change")
	}
	if !ParseScope("md").Matches([]string{"docs/ARCHITECTURE.md"}) {
		t.Error("a markdown rule must apply to a markdown change")
	}
}

func TestScopeMatchesDirectoryGlobs(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"internal/store/**", "internal/store/user.go", true},
		{"internal/store/**", "internal/billing/invoice.go", false},
		{"internal/**/*.go", "internal/a/b/c.go", true},
		{"*.go", "internal/store/user.go", true},  // base-name fallback
		{"*.go", "internal/store/user.ts", false}, //
		{"cmd/*/main.go", "cmd/review/main.go", true},
		{"cmd/*/main.go", "cmd/a/b/main.go", false}, // * does not cross /
		{"**/*.go", "main.go", true},               // **/ may match zero dirs
		{"docs/**", "docs/architecture/ARCHITECTURE.md", true},
		{"?.go", "a.go", true},
		{"?.go", "ab.go", false},
	}
	for _, c := range cases {
		got := ParseScope(c.pattern).Matches([]string{c.path})
		if got != c.want {
			t.Errorf("ParseScope(%q).Matches([%q]) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestScopeMatchesAnyPathInTheChange(t *testing.T) {
	s := ParseScope("md")
	mixed := []string{"internal/store/user.go", "docs/ARCHITECTURE.md"}
	if !s.Matches(mixed) {
		t.Error("a rule governing any one changed path must apply to the change")
	}
}

func TestScopeOfReadsMetadata(t *testing.T) {
	meta := NewMetadata()
	meta.Set(AppliesToKey, "go")
	if !ScopeOf(meta).Matches([]string{"main.go"}) {
		t.Error("ScopeOf should read applies_to from metadata")
	}
	if !ScopeOf(NewMetadata()).Universal() {
		t.Error("a document with no applies_to must be universal")
	}
}
