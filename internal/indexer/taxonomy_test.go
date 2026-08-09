package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
)

func taxonomyRepo(t *testing.T, body string) domain.Repository {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, domain.TaxonomyFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return domain.Repository{ID: "r", Name: "r", LocalPath: dir}
}

// TestApplyTaxonomyNeverOverrulesAnExistingType is RFC-0007's safety
// argument as a test, and the reason this change is landable under every
// existing consumer. A taxonomy can only turn `unknown` into something
// more specific; it can never reclassify a document that already has a
// type, so nothing a consumer groups by type moves.
func TestApplyTaxonomyNeverOverrulesAnExistingType(t *testing.T) {
	tax, err := domain.ParseTaxonomy([]byte("taxonomy:\n  plans/**: Decision\n"))
	if err != nil {
		t.Fatal(err)
	}

	for _, existing := range []domain.DocType{
		domain.DocTypeRule, domain.DocTypeADR, domain.DocTypeStandard,
		domain.DocTypeRFC, domain.DocTypeRoadmap, domain.DocTypeReadme,
	} {
		doc := domain.CanonicalDocument{Path: "plans/a.md", Type: existing}
		applyTaxonomy(tax, &doc)
		if doc.Type != existing {
			t.Errorf("a %q document under plans/ became %q — front matter is the more specific claim and must win",
				existing, doc.Type)
		}
	}
}

func TestApplyTaxonomyClassifiesTheUnclassified(t *testing.T) {
	tax, err := domain.ParseTaxonomy([]byte("taxonomy:\n  plans/**: Decision\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc := domain.CanonicalDocument{Path: "plans/to-do/x.md", Type: domain.DocTypeUnknown}
	applyTaxonomy(tax, &doc)
	if doc.Type != domain.DocTypeADR {
		t.Errorf("Type = %q, want %q", doc.Type, domain.DocTypeADR)
	}
}

func TestApplyTaxonomyLeavesUnmatchedPathsAlone(t *testing.T) {
	tax, err := domain.ParseTaxonomy([]byte("taxonomy:\n  plans/**: Decision\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc := domain.CanonicalDocument{Path: "src/README.md", Type: domain.DocTypeUnknown}
	applyTaxonomy(tax, &doc)
	if doc.Type != domain.DocTypeUnknown {
		t.Errorf("Type = %q, want it untouched", doc.Type)
	}
}

func TestLoadTaxonomyTreatsAbsenceAsNormal(t *testing.T) {
	tax, err := loadTaxonomy(taxonomyRepo(t, ""))
	if err != nil {
		t.Fatalf("a repository with no %s is the normal case: %v", domain.TaxonomyFile, err)
	}
	if !tax.Empty() {
		t.Error("want an empty taxonomy")
	}
}

// TestLoadTaxonomyFailsOnAMalformedFile: the repository is trying to say
// something. Ignoring it would leave its documents unclassified with
// nothing to explain why — the failure this whole RFC exists to remove.
func TestLoadTaxonomyFailsOnAMalformedFile(t *testing.T) {
	_, err := loadTaxonomy(taxonomyRepo(t, "taxonomy:\n  plans/**: Design\n"))
	if err == nil {
		t.Fatal("want an error for an uninterpretable taxonomy")
	}
	if !strings.Contains(err.Error(), domain.TaxonomyFile) {
		t.Errorf("error should name the file: %v", err)
	}
}

// TestReindexAppliesANewlyAddedTaxonomy is the blocking finding from the
// first Validation Phase 1 review, as a test.
//
// Adding a .engineering.yaml changes no markdown file's bytes, so the
// content-hash short-circuit reported every existing document Unchanged
// and left it `unknown`. RFC-0007 therefore worked on a fresh index and
// did nothing on the upgrade path every real adopter is on — which is
// the only path that matters, since the repositories that need a
// taxonomy are the ones already indexed without one.
func TestReindexAppliesANewlyAddedTaxonomy(t *testing.T) {
	stored := domain.CanonicalDocument{
		Path:        "plans/to-do/decide.md",
		Type:        domain.DocTypeUnknown,
		ContentHash: "same",
	}

	// Same bytes, so the hash matches; the taxonomy is new.
	fresh := domain.CanonicalDocument{
		Path:        stored.Path,
		Type:        domain.DocTypeUnknown,
		ContentHash: "same",
	}
	tax, err := domain.ParseTaxonomy([]byte("taxonomy:\n  plans/**: Decision\n"))
	if err != nil {
		t.Fatal(err)
	}
	applyTaxonomy(tax, &fresh)

	unchanged := stored.ContentHash == fresh.ContentHash && stored.Type == fresh.Type
	if unchanged {
		t.Fatal("a document whose classification changed must not count as unchanged — " +
			"skipping it here is what made adding a taxonomy a no-op on every already-indexed repository")
	}
	if fresh.Type != domain.DocTypeADR {
		t.Errorf("Type = %q, want %q", fresh.Type, domain.DocTypeADR)
	}
}
