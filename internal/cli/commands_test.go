package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
)

// workspaceWith builds a workspace root holding named repositories, the
// layout the documentation recommends: the root is a container, detached
// on purpose, and the repositories hang under it.
func workspaceWith(t *testing.T, repos map[string]map[string]string) string {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	var out strings.Builder

	if err := WorkspaceCreate(ctx, root, &out); err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
	}
	if err := WorkspaceDetach(ctx, root, root, &out); err != nil {
		t.Fatalf("WorkspaceDetach: %v", err)
	}
	for name, files := range repos {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for rel, body := range files {
			writeFile(t, dir, rel, body)
		}
		if err := WorkspaceAttach(ctx, root, dir, &out); err != nil {
			t.Fatalf("WorkspaceAttach %s: %v", name, err)
		}
	}
	return root
}

const ruleDoc = "---\ndoc: RULE\nid: no-raw-sql\napplies_to: \"**/*.go\"\n---\n\n# Rule\n\nNo raw SQL.\n"

// TestStatusSaysWhenNoRulebookIsPresent.
//
// A workspace holding an application and no rules answers every question
// about rules with a confident "none", and that is indistinguishable from
// a correct answer. Counts alone let a developer conclude everything is
// fine, which is how three workspaces holding 64 documents and zero rules
// survived on one machine until Sprint 11 went looking.
func TestStatusSaysWhenNoRulebookIsPresent(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app": {"README.md": "---\ndoc: README\n---\n\n# App\n"},
	})

	var out strings.Builder
	if err := Status(context.Background(), root, &out); err != nil {
		t.Fatalf("Status: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Rules:     0") {
		t.Errorf("status must report the rule count, especially when it is zero:\n%s", got)
	}
	if !strings.Contains(got, "nothing governs these files") {
		t.Errorf("status must say what a zero rule count means for a review:\n%s", got)
	}
}

func TestStatusReportsRulesWhenPresent(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app":       {"main.go.md": "---\ndoc: README\n---\n\n# App\n"},
		"knowledge": {"rules/no-raw-sql.md": ruleDoc},
	})

	var out strings.Builder
	if err := Status(context.Background(), root, &out); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Rules:     1 indexed") {
		t.Errorf("status did not report the indexed rule:\n%s", got)
	}
}

// TestWorkspaceStatusNamesTheMissingRulebook is the same argument at the
// workspace level, where `eng doctor` falls back to it when the transport
// is not installed.
func TestWorkspaceStatusNamesTheMissingRulebook(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app": {"README.md": "---\ndoc: README\n---\n\n# App\n"},
	})

	var out strings.Builder
	if err := WorkspaceStatus(context.Background(), root, &out); err != nil {
		t.Fatalf("WorkspaceStatus: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Repositories: 1", "Rules:        0", "eng workspace attach"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// TestCleanRefusesWithoutConfirmation: the index rebuilds from documents,
// but the list of attached repositories exists nowhere else. Cleaning a
// multi-repository workspace destroys the setup, not the data, and the
// developer has to be told which repositories they are about to have to
// re-attach.
func TestCleanRefusesWithoutConfirmation(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app":       {"README.md": "---\ndoc: README\n---\n\n# App\n"},
		"knowledge": {"rules/r.md": ruleDoc},
	})

	var out strings.Builder
	if err := Clean(context.Background(), root, &out, false); err == nil {
		t.Fatal("Clean without confirmation must not proceed")
	}
	got := out.String()
	for _, want := range []string{"app", "knowledge", "--yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal should name %q so the reader knows the cost:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(root, dbDirName)); err != nil {
		t.Error("Clean deleted the workspace despite refusing")
	}
}

func TestCleanRemovesTheWorkspaceWhenConfirmed(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app": {"README.md": "---\ndoc: README\n---\n\n# App\n"},
	})

	var out strings.Builder
	if err := Clean(context.Background(), root, &out, true); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, dbDirName)); !os.IsNotExist(err) {
		t.Error("Clean did not remove .eng/")
	}
}

func TestCleanOnAnAbsentWorkspaceIsNotAnError(t *testing.T) {
	var out strings.Builder
	if err := Clean(context.Background(), t.TempDir(), &out, true); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !strings.Contains(out.String(), "Nothing to clean") {
		t.Errorf("got %q", out.String())
	}
}

// TestTaxonomyShowsTheDeclaredNames: several canonical names collapse
// onto one internal DocType — Reference is stored as readme, Planning as
// roadmap — so printing the DocType would show the author a word they did
// not write and cannot find in the documentation.
func TestTaxonomyShowsTheDeclaredNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, domain.TaxonomyFile, "taxonomy:\n  docs/**: Reference\n  plans/**: Planning\n")

	var out strings.Builder
	if err := Taxonomy(context.Background(), dir, &out); err != nil {
		t.Fatalf("Taxonomy: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Reference", "Planning"} {
		if !strings.Contains(got, want) {
			t.Errorf("output should use the author's own vocabulary, missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"readme", "roadmap"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output leaked the internal type %q:\n%s", unwanted, got)
		}
	}
}

func TestTaxonomyExplainsItselfWhenAbsent(t *testing.T) {
	var out strings.Builder
	if err := Taxonomy(context.Background(), t.TempDir(), &out); err != nil {
		t.Fatalf("Taxonomy: %v", err)
	}
	got := out.String()
	for _, want := range []string{domain.TaxonomyFile, "Decision", "Write it yourself", "eng index"} {
		if !strings.Contains(got, want) {
			t.Errorf("an absent taxonomy should teach, missing %q:\n%s", want, got)
		}
	}
}

func TestTaxonomyValidateRejectsAnUnknownType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, domain.TaxonomyFile, "taxonomy:\n  plans/**: Design\n")

	var out strings.Builder
	err := TaxonomyValidate(context.Background(), dir, &out)
	if err == nil {
		t.Fatal("want an error for a type outside the canonical set")
	}
	if !strings.Contains(err.Error(), "Design") {
		t.Errorf("the error should name the offending value: %v", err)
	}
}

// TestTaxonomyValidateCountsWhatItWouldClassify is what makes validate
// more than a parse. A file can be perfectly well-formed and match
// nothing, which looks exactly like not having written one.
func TestTaxonomyValidateCountsWhatItWouldClassify(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app": {
			"plans/decide.md": "# A plan with no front matter\n",
			"README.md":       "---\ndoc: README\n---\n\n# App\n",
		},
	})
	app := filepath.Join(root, "app")
	writeFile(t, app, domain.TaxonomyFile, "taxonomy:\n  plans/**: Decision\n")

	var out strings.Builder
	if err := TaxonomyValidate(context.Background(), app, &out); err != nil {
		t.Fatalf("TaxonomyValidate: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "1 still unknown and claimed by this file") {
		t.Errorf("validate should count the documents the file would rescue:\n%s", got)
	}
}

func TestTaxonomyValidateSaysSoWhenItMatchesNothing(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app": {"notes/thoughts.md": "# Unclassified\n"},
	})
	app := filepath.Join(root, "app")
	writeFile(t, app, domain.TaxonomyFile, "taxonomy:\n  plans/**: Decision\n")

	var out strings.Builder
	if err := TaxonomyValidate(context.Background(), app, &out); err != nil {
		t.Fatalf("TaxonomyValidate: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "matches nothing") {
		t.Errorf("a valid taxonomy that claims nothing must say so:\n%s", got)
	}
}

// TestConfigResolvesTheWorkspaceUpward: the recommended layout puts the
// workspace above the repositories, so a command that only looked in the
// current directory would report "none" from inside an indexed one.
func TestConfigResolvesTheWorkspaceUpward(t *testing.T) {
	root := workspaceWith(t, map[string]map[string]string{
		"app": {"README.md": "---\ndoc: README\n---\n\n# App\n"},
	})

	var out strings.Builder
	if err := Config(context.Background(), filepath.Join(root, "app"), &out); err != nil {
		t.Fatalf("Config: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "workspace") || !strings.Contains(got, root) {
		t.Errorf("config should resolve the workspace above this directory:\n%s", got)
	}
	// eng has no os.Getenv call; naming the variable without that caveat
	// is what sent a reader to export something that changes nothing.
	if !strings.Contains(got, "not by eng") {
		t.Errorf("config must say ENGINEERING_WORKSPACE is not read by eng:\n%s", got)
	}
}

// TestReviewRefusesOutsideAWorkspace: the command's whole purpose is to
// check the setup before handing over, so an unusable setup must stop it
// rather than launch a review that silently cites nothing.
func TestReviewRefusesOutsideAWorkspace(t *testing.T) {
	var out strings.Builder
	err := Review(context.Background(), t.TempDir(), &out, false)
	if err == nil {
		t.Fatal("Review must not hand over from a directory with no workspace")
	}
	if !strings.Contains(err.Error(), "git repository") && !strings.Contains(err.Error(), "workspace") {
		t.Errorf("the error should name what is missing: %v", err)
	}
}

// TestSilentPassesThroughOrdinaryErrors: only a subcommand that already
// printed its own diagnosis suppresses main's message.
func TestSilentPassesThroughOrdinaryErrors(t *testing.T) {
	if Silent(os.ErrNotExist) {
		t.Error("an ordinary error must still be printed")
	}
	if !Silent(errSilent{os.ErrNotExist}) {
		t.Error("a command that reported for itself should not be reported twice")
	}
}
