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
