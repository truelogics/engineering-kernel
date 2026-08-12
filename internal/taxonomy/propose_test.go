package taxonomy

import (
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
)

func docs(pairs ...any) []domain.CanonicalDocument {
	out := make([]domain.CanonicalDocument, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, domain.CanonicalDocument{
			Path: pairs[i].(string),
			Type: pairs[i+1].(domain.DocType),
		})
	}
	return out
}

const unknown = domain.DocTypeUnknown

func patterns(p Proposal) []string {
	out := make([]string, 0, len(p.Mappings))
	for _, m := range p.Mappings {
		out = append(out, m.Pattern+"="+m.Name)
	}
	return out
}

// TestEveryProposableNameIsCanonical: the proposer carries its own table
// of directory names, and a value that is not in domain's canonical set
// would render a file that ParseTaxonomy rejects — a proposal that cannot
// be applied.
func TestEveryProposableNameIsCanonical(t *testing.T) {
	for dir, name := range directoryNames {
		if _, ok := domain.CanonicalType(name); !ok {
			t.Errorf("%q proposes %q, which is not a canonical type", dir, name)
		}
	}
}

// TestProposalAlwaysParses closes the loop the other way: whatever the
// proposer emits must be readable by the code that will apply it.
func TestProposalAlwaysParses(t *testing.T) {
	p := Propose(docs(
		"plans/a.md", unknown, "handbook/b.md", unknown, "specs/c.md", unknown,
	), domain.Taxonomy{})
	if p.Empty() {
		t.Fatal("expected a proposal")
	}
	tax, err := domain.ParseTaxonomy([]byte(p.YAML()))
	if err != nil {
		t.Fatalf("the proposal does not parse:\n%s\n%v", p.YAML(), err)
	}
	if len(tax.Mappings()) != len(p.Mappings) {
		t.Errorf("rendered %d mappings, parsed %d", len(p.Mappings), len(tax.Mappings()))
	}
}

func TestProposesFromDirectoryNames(t *testing.T) {
	p := Propose(docs(
		"plans/q1.md", unknown,
		"handbook/oncall.md", unknown,
		"docs/architecture/overview.md", unknown,
	), domain.Taxonomy{})
	got := strings.Join(patterns(p), " ")
	for _, want := range []string{"plans/**=Planning", "handbook/**=Guide", "docs/architecture/**=Architecture"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}

// TestLowConfidenceDirectoriesAreOmitted is the sprint's conservatism
// rule. The objective is useful proposals, not maximum classification: a
// wrong mapping returns confidently mislabelled documents, which is worse
// than leaving them unknown.
func TestLowConfidenceDirectoriesAreOmitted(t *testing.T) {
	p := Propose(docs(
		"weird-folder/thing.md", unknown,
		"notes/random.md", unknown,
		"misc/x.md", unknown,
	), domain.Taxonomy{})
	if !p.Empty() {
		t.Errorf("nothing here identifies itself; proposed %v", patterns(p))
	}
	if len(p.Skipped) == 0 {
		t.Error("skipped directories must be reported, not silently dropped")
	}
	for _, s := range p.Skipped {
		if s.Reason == "" {
			t.Errorf("%s was skipped without a reason", s.Pattern)
		}
	}
}

// TestAmbiguousParentIsSkippedButSpecificChildIsNot: `docs/` holds
// whatever a project puts in it. `docs/architecture/` does not.
func TestAmbiguousParentIsSkippedButSpecificChildIsNot(t *testing.T) {
	p := Propose(docs(
		"docs/architecture/a.md", unknown,
		"docs/random-thoughts.md", unknown,
	), domain.Taxonomy{})
	got := strings.Join(patterns(p), " ")
	if !strings.Contains(got, "docs/architecture/**=Architecture") {
		t.Errorf("want the specific child proposed, got %v", got)
	}
	if strings.Contains(got, "docs/**=") {
		t.Errorf("`docs` is ambiguous and must not be proposed for: %v", got)
	}
}

// TestFrontMatterMajorityIsEvidence: a directory whose documents mostly
// declare what they are tells you about its unclassified siblings, even
// when its name means nothing.
func TestFrontMatterMajorityIsEvidence(t *testing.T) {
	p := Propose(docs(
		"weird-folder/a.md", domain.DocTypeGuide,
		"weird-folder/b.md", domain.DocTypeGuide,
		"weird-folder/c.md", unknown,
	), domain.Taxonomy{})
	if got := patterns(p); len(got) != 1 || got[0] != "weird-folder/**=Guide" {
		t.Errorf("front matter should carry an unnamed directory, got %v", got)
	}
}

// TestOneTypedDocumentIsNotAMajority: a single typed document among
// unknowns is a coincidence, and a proposal resting on a coincidence is
// the mislabelling this is meant to avoid.
func TestOneTypedDocumentIsNotAMajority(t *testing.T) {
	p := Propose(docs(
		"weird-folder/a.md", domain.DocTypeGuide,
		"weird-folder/b.md", unknown,
		"weird-folder/c.md", unknown,
	), domain.Taxonomy{})
	if !p.Empty() {
		t.Errorf("one document is not evidence about seven others: %v", patterns(p))
	}
}

// TestNameAndFrontMatterConflictIsSkipped: either could be right, and
// nothing here can tell which.
func TestNameAndFrontMatterConflictIsSkipped(t *testing.T) {
	p := Propose(docs(
		"handbook/a.md", domain.DocTypeADR,
		"handbook/b.md", domain.DocTypeADR,
		"handbook/c.md", unknown,
	), domain.Taxonomy{})
	if !p.Empty() {
		t.Errorf("a name/front-matter conflict must not be resolved by guessing: %v", patterns(p))
	}
	if len(p.Skipped) != 1 || !strings.Contains(p.Skipped[0].Reason, "Decision") {
		t.Errorf("the conflict should be explained: %+v", p.Skipped)
	}
}

// TestDirectoriesTheParserAlreadyClassifiesAreNotProposedFor.
//
// The markdown parser infers a type from `rules/`, `adr/`, `rfc/` and
// `roadmap/` segments itself. Proposing for them would duplicate a
// built-in rule and classify nothing; the "must rescue an unknown
// document" condition removes them without a special case.
func TestDirectoriesTheParserAlreadyClassifiesAreNotProposedFor(t *testing.T) {
	p := Propose(docs(
		"rules/a.md", domain.DocTypeRule,
		"rules/b.md", domain.DocTypeRule,
		"plans/c.md", unknown,
	), domain.Taxonomy{})
	if got := patterns(p); len(got) != 1 || got[0] != "plans/**=Planning" {
		t.Errorf("only the directory with something to gain should be proposed for, got %v", got)
	}
}

// TestRedundantChildIsNotProposed: `plans/**` already covers
// `plans/2026/**` with the same meaning.
func TestRedundantChildIsNotProposed(t *testing.T) {
	p := Propose(docs("plans/a.md", unknown, "plans/2026/b.md", unknown), domain.Taxonomy{})
	if got := patterns(p); len(got) != 1 || got[0] != "plans/**=Planning" {
		t.Errorf("want one covering mapping, got %v", got)
	}
}

// TestImpactIsMeasuredNotEstimated. The sprint is explicit: do not claim
// exact results unless they are actually calculated. Propose renders the
// file, parses it with the real parser, and applies it with the real
// matcher — so these numbers are the numbers indexing will produce.
func TestImpactIsMeasuredNotEstimated(t *testing.T) {
	p := Propose(docs(
		"plans/a.md", unknown, "plans/b.md", unknown, "plans/c.md", unknown,
		"handbook/d.md", unknown,
		"weird/e.md", unknown,
	), domain.Taxonomy{})
	if p.UnknownBefore != 5 {
		t.Errorf("UnknownBefore = %d, want 5", p.UnknownBefore)
	}
	if p.UnknownAfter != 1 {
		t.Errorf("UnknownAfter = %d, want 1 — only weird/e.md is unclaimed", p.UnknownAfter)
	}
	for _, m := range p.Mappings {
		want := map[string]int{"plans/**": 3, "handbook/**": 1}[m.Pattern]
		if m.Classifies != want {
			t.Errorf("%s classifies %d, want %d", m.Pattern, m.Classifies, want)
		}
	}
}

// TestFrontMatterOverridesAreReported: RFC-0007's precedence is not
// changed here, so a pattern a document contradicts does nothing — and
// the developer approving that pattern should be told.
func TestFrontMatterOverridesAreReported(t *testing.T) {
	p := Propose(docs(
		"plans/a.md", unknown,
		"plans/b.md", unknown,
		"plans/decided.md", domain.DocTypeRule,
	), domain.Taxonomy{})
	if len(p.Overridden) != 1 || p.Overridden[0] != "plans/decided.md" {
		t.Errorf("Overridden = %v, want [plans/decided.md]", p.Overridden)
	}
	for _, m := range p.Mappings {
		if m.Pattern == "plans/**" && m.Classifies != 2 {
			t.Errorf("the overridden document must not be counted as classified: %d", m.Classifies)
		}
	}
}

// TestProposalIsDeterministic. A proposal that shuffled between runs
// could not be reviewed in a diff, and the whole design rests on being
// reproducible rather than generated.
func TestProposalIsDeterministic(t *testing.T) {
	input := docs(
		"plans/a.md", unknown, "handbook/b.md", unknown, "specs/c.md", unknown,
		"docs/architecture/d.md", unknown, "weird/e.md", unknown,
		"guides/f.md", unknown, "reference/g.md", unknown,
	)
	first := Propose(input, domain.Taxonomy{}).YAML()
	for i := 0; i < 20; i++ {
		if got := Propose(input, domain.Taxonomy{}).YAML(); got != first {
			t.Fatalf("run %d differed:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
	if !strings.Contains(first, "taxonomy:") {
		t.Errorf("not a taxonomy file:\n%s", first)
	}
}

// TestNothingToProposeWhenEverythingIsClassified.
func TestNothingToProposeWhenEverythingIsClassified(t *testing.T) {
	p := Propose(docs("plans/a.md", domain.DocTypeRoadmap, "rules/b.md", domain.DocTypeRule), domain.Taxonomy{})
	if !p.Empty() || p.YAML() != "" {
		t.Errorf("nothing is unknown, so nothing is worth proposing: %v", patterns(p))
	}
}

// TestUnnameableTypeIsNotProposed: the parser infers DocTypeRFC from a
// path, and the canonical vocabulary has no separate word for it. A
// proposal must not invent one.
func TestUnnameableTypeIsNotProposed(t *testing.T) {
	p := Propose(docs(
		"proposals/a.md", domain.DocTypeRFC,
		"proposals/b.md", domain.DocTypeRFC,
		"proposals/c.md", unknown,
	), domain.Taxonomy{})
	if !p.Empty() {
		t.Errorf("DocTypeRFC has no canonical name; proposed %v", patterns(p))
	}
}

// TestRootIsNeverProposedFor: a mapping of `**` would claim an entire
// repository on the strength of whatever its most common directory held.
func TestRootIsNeverProposedFor(t *testing.T) {
	p := Propose(docs("a.md", unknown, "b.md", unknown, "c.md", unknown), domain.Taxonomy{})
	for _, m := range p.Mappings {
		if m.Pattern == "**" || m.Pattern == "./**" {
			t.Errorf("proposed a repository-wide mapping: %+v", m)
		}
	}
}
