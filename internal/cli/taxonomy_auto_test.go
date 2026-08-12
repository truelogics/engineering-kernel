package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
)

// repoWithLayout writes a repository with no workspace and no index —
// the state `eng taxonomy auto` is designed to run in, straight after
// `eng init`.
func repoWithLayout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		writeFile(t, dir, rel, body)
	}
	return dir
}

var messyRepo = map[string]string{
	"plans/q1.md":                 "# Q1\n",
	"plans/q2.md":                 "# Q2\n",
	"handbook/oncall.md":          "# On call\n",
	"handbook/onboarding.md":      "# Onboarding\n",
	"docs/architecture/system.md": "# System\n",
	"weird-folder/thing.md":       "# ?\n",
	"weird-folder/other-thing.md": "# ?\n",
	"rules/no-raw-sql.md":         ruleDoc,
}

func taxonomyPath(dir string) string { return filepath.Join(dir, domain.TaxonomyFile) }

// TestAutoProposesWithoutAnIndex is the Definition of Done's flow: `eng
// init` creates a workspace and indexes nothing, so a proposal that
// needed an index would have nothing to read at the moment it is asked.
func TestAutoProposesWithoutAnIndex(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("n\n"), &out, false, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	got := out.String()
	for _, want := range []string{"plans/**", "handbook/**", "docs/architecture/**", "Apply this taxonomy?"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// TestAutoRequiresConfirmationBeforeWriting is the sprint's core
// principle. Automatic generation is a proposal; the file is the
// repository owner's.
func TestAutoRequiresConfirmationBeforeWriting(t *testing.T) {
	for name, answer := range map[string]string{
		"declined":     "n\n",
		"empty line":   "\n",
		"closed stdin": "",
		"ambiguous":    "maybe\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := repoWithLayout(t, messyRepo)
			var out strings.Builder
			if err := TaxonomyAuto(context.Background(), dir, strings.NewReader(answer), &out, false, false); err != nil {
				t.Fatalf("TaxonomyAuto: %v", err)
			}
			if _, err := os.Stat(taxonomyPath(dir)); !os.IsNotExist(err) {
				t.Fatalf("%s was written without a yes", domain.TaxonomyFile)
			}
			if !strings.Contains(out.String(), "unchanged") {
				t.Errorf("the developer should be told nothing happened:\n%s", out.String())
			}
		})
	}
}

func TestAutoWritesOnlyAfterYes(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, false, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	content, err := os.ReadFile(taxonomyPath(dir))
	if err != nil {
		t.Fatalf("accepted, but nothing was written: %v", err)
	}
	// The written file must be readable by the code that will apply it.
	tax, err := domain.ParseTaxonomy(content)
	if err != nil {
		t.Fatalf("wrote a file that does not parse:\n%s\n%v", content, err)
	}
	if tax.Empty() {
		t.Error("wrote an empty taxonomy")
	}
	if got := out.String(); !strings.Contains(got, "Run `eng index` to apply it") {
		t.Errorf("the developer should be told the next step:\n%s", got)
	}
}

// TestAutoNeverOverwritesAnExistingTaxonomy: the file is a repository
// owner's statement, and a command that replaced it on the way past would
// discard a decision nobody was asked about.
func TestAutoNeverOverwritesAnExistingTaxonomy(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)
	original := "taxonomy:\n  plans/**: Guide\n"
	writeFile(t, dir, domain.TaxonomyFile, original)

	var out strings.Builder
	// Answering yes as well, so the test proves the guard is the file's
	// existence and not the absence of consent.
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, false, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	after, err := os.ReadFile(taxonomyPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("the existing taxonomy was modified:\n%s", after)
	}
	if !strings.Contains(out.String(), "--update") {
		t.Errorf("the developer should be told how to proceed deliberately:\n%s", out.String())
	}
}

// TestAutoUpdateStillAsks: --update is how you opt into a rewrite, not
// how you skip the question.
func TestAutoUpdateStillAsks(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)
	original := "taxonomy:\n  plans/**: Guide\n"
	writeFile(t, dir, domain.TaxonomyFile, original)

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("n\n"), &out, true, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	after, _ := os.ReadFile(taxonomyPath(dir))
	if string(after) != original {
		t.Errorf("--update wrote without a yes:\n%s", after)
	}
	got := out.String()
	if !strings.Contains(got, "Apply this taxonomy?") {
		t.Errorf("--update must still ask:\n%s", got)
	}
	// The developer is approving a rewrite of their own file, so the
	// change has to be visible without re-reading the whole thing.
	if !strings.Contains(got, "Against your existing file") || !strings.Contains(got, "Guide → Planning") {
		t.Errorf("--update should show what changes:\n%s", got)
	}
}

// TestAutoRefusesToOverwriteAFileItCannotRead: a malformed taxonomy is a
// statement the repository was trying to make. Replacing it because it
// did not parse would destroy the evidence of the mistake along with it.
func TestAutoRefusesToOverwriteAFileItCannotRead(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)
	broken := "taxonomy:\n  plans/**: Design\n"
	writeFile(t, dir, domain.TaxonomyFile, broken)

	var out strings.Builder
	err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, true, false)
	if err == nil {
		t.Fatal("want an error for a taxonomy that does not parse")
	}
	after, _ := os.ReadFile(taxonomyPath(dir))
	if string(after) != broken {
		t.Errorf("the unreadable file was overwritten:\n%s", after)
	}
}

// TestAutoOmitsLowConfidenceDirectoriesAndSaysSo.
func TestAutoOmitsLowConfidenceDirectoriesAndSaysSo(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, false, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	content, _ := os.ReadFile(taxonomyPath(dir))
	if strings.Contains(string(content), "weird-folder") {
		t.Errorf("guessed at a directory it cannot identify:\n%s", content)
	}
	if !strings.Contains(out.String(), "weird-folder/**") {
		t.Errorf("what was skipped, and why, must be reported:\n%s", out.String())
	}
}

// TestAutoReportsNothingToProposeRatherThanWritingAnEmptyFile.
func TestAutoReportsNothingToProposeRatherThanWritingAnEmptyFile(t *testing.T) {
	dir := repoWithLayout(t, map[string]string{"rules/a.md": ruleDoc})

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, false, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	if _, err := os.Stat(taxonomyPath(dir)); !os.IsNotExist(err) {
		t.Error("wrote a file with nothing in it to propose")
	}
	if !strings.Contains(out.String(), "already classified") {
		t.Errorf("should say why there is nothing to do:\n%s", out.String())
	}
}

// TestAutoValidatesWhatItWroteFromDisk. The report has to describe the
// file that is now on disk, since that is what `eng index` will read.
func TestAutoValidatesWhatItWroteFromDisk(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, false, false); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"valid YAML", "all canonical types",
		"classified by this file", "still unknown", "front matter overrides",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the post-write report is missing %q:\n%s", want, got)
		}
	}
}

// TestAutoIsDeterministicAcrossRuns: two developers on the same commit
// must get the same file, or the proposal is not reviewable.
func TestAutoIsDeterministicAcrossRuns(t *testing.T) {
	var first string
	for i := 0; i < 5; i++ {
		dir := repoWithLayout(t, messyRepo)
		var out strings.Builder
		if err := TaxonomyAuto(context.Background(), dir, strings.NewReader("y\n"), &out, false, false); err != nil {
			t.Fatalf("TaxonomyAuto: %v", err)
		}
		content, err := os.ReadFile(taxonomyPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(content)
			continue
		}
		if string(content) != first {
			t.Fatalf("run %d differed:\n--- first ---\n%s\n--- got ---\n%s", i, first, content)
		}
	}
}

// TestAutoYesFlagIsExplicitApproval: --yes is a decision typed on the
// command line, which is the opposite of silent. It still must not apply
// to a repository that already has a taxonomy.
func TestAutoYesFlagIsExplicitApproval(t *testing.T) {
	dir := repoWithLayout(t, messyRepo)

	var out strings.Builder
	if err := TaxonomyAuto(context.Background(), dir, strings.NewReader(""), &out, false, true); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	if _, err := os.Stat(taxonomyPath(dir)); err != nil {
		t.Fatalf("--yes did not write the file: %v", err)
	}

	existing := repoWithLayout(t, messyRepo)
	writeFile(t, existing, domain.TaxonomyFile, "taxonomy:\n  plans/**: Guide\n")
	var out2 strings.Builder
	if err := TaxonomyAuto(context.Background(), existing, strings.NewReader(""), &out2, false, true); err != nil {
		t.Fatalf("TaxonomyAuto: %v", err)
	}
	after, _ := os.ReadFile(taxonomyPath(existing))
	if !strings.Contains(string(after), "Guide") {
		t.Errorf("--yes overwrote an existing taxonomy without --update:\n%s", after)
	}
}
