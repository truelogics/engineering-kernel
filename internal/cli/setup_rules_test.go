package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// oneRepoProject is the layout most teams actually have: handbook, docs,
// plans and code in a single repository, and no separate rulebook
// anywhere. None of its documents carry `doc:` front matter.
func oneRepoProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeRepo(t, dir, map[string]string{
		"handbook/go-style.md":    "# How we write Go\nAlways wrap errors with context.\n",
		"handbook/code-review.md": "# Code review\nTwo approvals before merge.\n",
		"docs/architecture.md":    "# Architecture\nThe API talks to Postgres.\n",
		"plans/q3.md":             "# Q3 plan\nShip billing.\n",
		"src/main.go":             "package main\n",
	})
	return dir
}

func ruleCount(t *testing.T, dir string) int {
	t.Helper()
	n, counted := indexedRuleCount(context.Background(), dir)
	if !counted {
		t.Fatalf("could not count rules in %s", dir)
	}
	return n
}

// The gap this flag closes. `eng setup .` indexes the repository and
// finds no rules, because nothing in it says which directory holds them
// — and `eng taxonomy auto` does not close it either: it reads
// `handbook/` as Guide, which is what a handbook usually is.
func TestRulesDirTurnsADirectoryIntoRules(t *testing.T) {
	ctx := context.Background()
	dir := oneRepoProject(t)
	var out strings.Builder

	if _, err := prepareWorkspace(ctx, dir, nil, &out); err != nil {
		t.Fatal(err)
	}
	if n := ruleCount(t, dir); n != 0 {
		t.Fatalf("before: %d rules, want 0 — the fixture is not reproducing the problem", n)
	}

	if err := declareRules(ctx, dir, []string{"handbook"}, strings.NewReader(""), &out, true); err != nil {
		t.Fatalf("declareRules: %v", err)
	}
	if n := ruleCount(t, dir); n != 2 {
		t.Errorf("after: %d rules, want 2\n%s", n, out.String())
	}
}

// Writing the file is not enough — the taxonomy only takes effect on the
// next index. Stopping before it would report success over a workspace
// that still held zero rules, which is the failure being fixed.
func TestRulesDirReindexesSoItTakesEffect(t *testing.T) {
	ctx := context.Background()
	dir := oneRepoProject(t)
	var out strings.Builder
	if _, err := prepareWorkspace(ctx, dir, nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := declareRules(ctx, dir, []string{"handbook"}, strings.NewReader(""), &out, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Re-indexing") {
		t.Error("did not re-index after writing the taxonomy")
	}
}

// Nothing is written without a yes (RFC-0009). A closed stdin is a no,
// so an automated context never writes by default.
func TestRulesDirNeverWritesWithoutConsent(t *testing.T) {
	ctx := context.Background()
	dir := oneRepoProject(t)
	var out strings.Builder
	if _, err := prepareWorkspace(ctx, dir, nil, &out); err != nil {
		t.Fatal(err)
	}

	for _, answer := range []string{"n\n", "", "no\n"} {
		if err := declareRules(ctx, dir, []string{"handbook"}, strings.NewReader(answer), &out, false); err != nil {
			t.Fatalf("answer %q: %v", answer, err)
		}
		if _, err := os.Stat(filepath.Join(dir, domain.TaxonomyFile)); !os.IsNotExist(err) {
			t.Fatalf("answer %q wrote %s anyway", answer, domain.TaxonomyFile)
		}
	}
	if n := ruleCount(t, dir); n != 0 {
		t.Errorf("declining still produced %d rules", n)
	}
}

// Merges, never replaces. A line the developer wrote keeps its own
// wording, whatever it says — this adds a claim, it does not overrule
// one.
func TestRulesDirMergesWithAnExistingTaxonomy(t *testing.T) {
	ctx := context.Background()
	dir := oneRepoProject(t)
	mine := "taxonomy:\n  docs/**:      Architecture\n  handbook/**:  Guide\n"
	if err := os.WriteFile(filepath.Join(dir, domain.TaxonomyFile), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if _, err := prepareWorkspace(ctx, dir, nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := declareRules(ctx, dir, []string{"handbook", "plans"}, strings.NewReader(""), &out, true); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, domain.TaxonomyFile))
	if err != nil {
		t.Fatal(err)
	}
	tax, err := domain.ParseTaxonomy(content)
	if err != nil {
		t.Fatalf("wrote a file that does not parse: %v\n%s", err, content)
	}

	declared := map[string]string{}
	for _, m := range tax.Mappings() {
		declared[m.Pattern] = m.Declared
	}
	// The developer said handbook is a Guide. That stands.
	if declared["handbook/**"] != "Guide" {
		t.Errorf("overruled a line the developer wrote: handbook/** = %q, want Guide", declared["handbook/**"])
	}
	if declared["docs/**"] != "Architecture" {
		t.Errorf("lost an unrelated line: docs/** = %q, want Architecture", declared["docs/**"])
	}
	if declared["plans/**"] != "Rule" {
		t.Errorf("did not add the new one: plans/** = %q, want Rule", declared["plans/**"])
	}
}

// A typo must not silently declare nothing — that is indistinguishable
// from the problem the flag exists to fix.
func TestRulesDirRejectsADirectoryThatIsNotThere(t *testing.T) {
	ctx := context.Background()
	dir := oneRepoProject(t)
	var out strings.Builder
	if _, err := prepareWorkspace(ctx, dir, nil, &out); err != nil {
		t.Fatal(err)
	}

	err := declareRules(ctx, dir, []string{"handbok"}, strings.NewReader(""), &out, true)
	if err == nil {
		t.Fatal("a misspelled directory was accepted")
	}
	if !strings.Contains(err.Error(), "handbok") {
		t.Errorf("the error should name what was not found: %v", err)
	}
}

// A taxonomy belongs to a repository. Writing one into a container that
// holds several would produce a file no repository reads.
func TestRulesDirRefusesOnAContainerWorkspace(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir() // not a git repository
	writeRepo(t, filepath.Join(root, "app"), map[string]string{"handbook/x.md": "# x\n"})

	var out strings.Builder
	if _, err := prepareWorkspace(ctx, root, []string{filepath.Join(root, "app")}, &out); err != nil {
		t.Fatal(err)
	}

	err := declareRules(ctx, root, []string{"handbook"}, strings.NewReader(""), &out, true)
	if err == nil {
		t.Fatal("wrote a taxonomy into a container workspace")
	}
	if !strings.Contains(err.Error(), "cd ") {
		t.Errorf("the error should say where to run it instead: %v", err)
	}
}

// Re-running with the same directory is a no-op, not a duplicate line.
func TestRulesDirIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := oneRepoProject(t)
	var out strings.Builder
	if _, err := prepareWorkspace(ctx, dir, nil, &out); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := declareRules(ctx, dir, []string{"handbook"}, strings.NewReader(""), &out, true); err != nil {
			t.Fatal(err)
		}
	}
	content, _ := os.ReadFile(filepath.Join(dir, domain.TaxonomyFile))
	if n := strings.Count(string(content), "handbook/**"); n != 1 {
		t.Errorf("handbook/** appears %d times, want 1\n%s", n, content)
	}
	if !strings.Contains(out.String(), "Nothing to change") {
		t.Error("the second run should say there was nothing to change")
	}
}
