package domain

import (
	"strings"
	"testing"
)

func mustTaxonomy(t *testing.T, body string) Taxonomy {
	t.Helper()
	tax, err := ParseTaxonomy([]byte(body))
	if err != nil {
		t.Fatalf("ParseTaxonomy: %v", err)
	}
	return tax
}

// TestTypeForTheDocumentThatMotivatedThis is RFC-0007's first acceptance
// criterion. A planning document carrying a real design decision
// classified as `unknown`, so retrieval could only ever return it as one
// keyword hit among hundreds — while grep found it in a single call.
func TestTypeForTheDocumentThatMotivatedThis(t *testing.T) {
	tax := mustTaxonomy(t, `
taxonomy:
  plans/**: Decision
  handbook/**: Guide
`)
	got, ok := tax.TypeFor("plans/to-do/block-fractional-indexing-vorder-v2.md")
	if !ok {
		t.Fatal("no type for the plan that carried a blocking review finding")
	}
	if got != DocTypeADR {
		t.Errorf("TypeFor = %q, want %q — a decision must reach the section a reviewer reads for decisions", got, DocTypeADR)
	}
}

func TestTypeForMapsEveryCanonicalName(t *testing.T) {
	tax := mustTaxonomy(t, `
taxonomy:
  r/**: Rule
  d/**: Decision
  a/**: Architecture
  s/**: Specification
  g/**: Guide
  p/**: Planning
  f/**: Reference
  o/**: Other
`)
	for path, want := range map[string]DocType{
		"r/x.md": DocTypeRule,
		"d/x.md": DocTypeADR,
		"a/x.md": DocTypeStandard,
		"s/x.md": DocTypeSpecification,
		"g/x.md": DocTypeGuide,
		"p/x.md": DocTypeRoadmap,
		"f/x.md": DocTypeReadme,
		"o/x.md": DocTypeUnknown,
	} {
		if got, _ := tax.TypeFor(path); got != want {
			t.Errorf("TypeFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestParseTaxonomyIsCaseInsensitiveOnTypeNames(t *testing.T) {
	tax := mustTaxonomy(t, "taxonomy:\n  plans/**: decision\n  notes/**: REFERENCE\n")
	if got, _ := tax.TypeFor("plans/a.md"); got != DocTypeADR {
		t.Errorf("lowercase name: got %q", got)
	}
	if got, _ := tax.TypeFor("notes/a.md"); got != DocTypeReadme {
		t.Errorf("uppercase name: got %q", got)
	}
}

// TestParseTaxonomyRejectsAnUnknownType: a repository writing "Design"
// has made a statement the kernel cannot honour, and quietly dropping it
// would leave its documents unclassified with nothing to explain why.
func TestParseTaxonomyRejectsAnUnknownType(t *testing.T) {
	_, err := ParseTaxonomy([]byte("taxonomy:\n  plans/**: Design\n"))
	if err == nil {
		t.Fatal("want an error for a type outside the canonical set")
	}
	if !strings.Contains(err.Error(), "decision") {
		t.Errorf("error should list what is accepted, got: %v", err)
	}
}

func TestParseTaxonomyRejectsMalformedYAML(t *testing.T) {
	if _, err := ParseTaxonomy([]byte("taxonomy: [unclosed\n")); err == nil {
		t.Fatal("want an error for malformed YAML")
	}
}

func TestParseTaxonomyAcceptsAbsence(t *testing.T) {
	tax, err := ParseTaxonomy([]byte("# nothing here\n"))
	if err != nil {
		t.Fatalf("ParseTaxonomy: %v", err)
	}
	if !tax.Empty() {
		t.Error("a file with no taxonomy key says nothing")
	}
	if _, ok := tax.TypeFor("plans/a.md"); ok {
		t.Error("an empty taxonomy must not classify anything")
	}
}

// TestTypeForPrefersTheMoreSpecificPattern: two patterns can match one
// path, and a map iterates randomly. Longest pattern wins, so the result
// is both deterministic and the more specific statement.
func TestTypeForPrefersTheMoreSpecificPattern(t *testing.T) {
	tax := mustTaxonomy(t, `
taxonomy:
  plans/**: Planning
  plans/decisions/**: Decision
`)
	for i := 0; i < 20; i++ {
		if got, _ := tax.TypeFor("plans/decisions/a.md"); got != DocTypeADR {
			t.Fatalf("TypeFor = %q, want %q — the more specific pattern must win, every time", got, DocTypeADR)
		}
	}
	if got, _ := tax.TypeFor("plans/other/a.md"); got != DocTypeRoadmap {
		t.Errorf("TypeFor = %q, want %q", got, DocTypeRoadmap)
	}
}

func TestTypeForReportsNoMatch(t *testing.T) {
	tax := mustTaxonomy(t, "taxonomy:\n  plans/**: Decision\n")
	if _, ok := tax.TypeFor("src/main.go"); ok {
		t.Error("a path no pattern matches must report no match")
	}
}

func TestTypeForUsesAppliesToGlobSemantics(t *testing.T) {
	tax := mustTaxonomy(t, `
taxonomy:
  "docs/*/overview.md": Guide
  "**/RUNBOOK.md": Reference
`)
	if got, ok := tax.TypeFor("docs/billing/overview.md"); !ok || got != DocTypeGuide {
		t.Errorf("single-star segment: got %q ok=%v", got, ok)
	}
	if _, ok := tax.TypeFor("docs/a/b/overview.md"); ok {
		t.Error("* must not cross a path separator — that is RFC-0005's semantics")
	}
	if got, ok := tax.TypeFor("apps/api/RUNBOOK.md"); !ok || got != DocTypeReadme {
		t.Errorf("double-star: got %q ok=%v", got, ok)
	}
}
