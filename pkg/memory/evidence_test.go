package memory

import "testing"

func pkgWith(files ...FileContext) ContextPackage {
	return ContextPackage{RelevantFiles: files}
}

func TestVerifyEvidenceHighWhenExcerptIsInsideSnippet(t *testing.T) {
	p := pkgWith(FileContext{
		Path:       "rules/logging.md",
		Repository: "engineering",
		Snippet:    "All logging goes through internal/log. Never fmt.Println.",
	})
	ev, ok := p.VerifyEvidence("rules/logging.md", "goes through internal/log")
	if !ok {
		t.Fatal("an excerpt present in the snippet must verify")
	}
	if ev.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", ev.Confidence)
	}
	if ev.Repository != "engineering" {
		t.Errorf("Repository = %q, want engineering", ev.Repository)
	}
	if got := ev.Qualified(); got != "engineering:rules/logging.md" {
		t.Errorf("Qualified() = %q", got)
	}
}

func TestVerifyEvidenceMediumWhenSnippetIsInsideExcerpt(t *testing.T) {
	p := pkgWith(FileContext{Path: "a.md", Snippet: "never fmt.Println"})
	ev, ok := p.VerifyEvidence("a.md", "All logging goes through internal/log. Never fmt.Println. That is the rule.")
	if !ok {
		t.Fatal("a longer quote containing the snippet must verify")
	}
	if ev.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", ev.Confidence)
	}
}

func TestVerifyEvidenceRejectsAnInventedQuote(t *testing.T) {
	p := pkgWith(FileContext{Path: "a.md", Snippet: "All logging goes through internal/log."})
	if _, ok := p.VerifyEvidence("a.md", "All logging must be disabled in production"); ok {
		t.Fatal("an excerpt absent from the snippet must not verify — this is the anti-hallucination check")
	}
}

func TestVerifyEvidenceRejectsAnUnretrievedDocument(t *testing.T) {
	p := pkgWith(FileContext{Path: "a.md", Snippet: "text"})
	if _, ok := p.VerifyEvidence("never/retrieved.md", "text"); ok {
		t.Fatal("a document retrieval never returned must not verify")
	}
}

// TestVerifyEvidenceRefusesAmbiguousBarePath is the multi-repository
// case: two repositories both have README.md, and attributing a quote to
// whichever came first would credit a document nobody chose.
func TestVerifyEvidenceRefusesAmbiguousBarePath(t *testing.T) {
	p := pkgWith(
		FileContext{Path: "README.md", Repository: "engineering", Snippet: "the rules index"},
		FileContext{Path: "README.md", Repository: "ai-review", Snippet: "the review engine"},
	)
	if _, ok := p.VerifyEvidence("README.md", "the rules index"); ok {
		t.Fatal("an ambiguous bare path must not verify")
	}
	ev, ok := p.VerifyEvidence("engineering:README.md", "the rules index")
	if !ok {
		t.Fatal("a repository-qualified path must resolve the ambiguity")
	}
	if ev.Repository != "engineering" {
		t.Errorf("Repository = %q, want engineering", ev.Repository)
	}
}

func TestVerifyEvidenceIgnoresHighlightMarkupAndWhitespace(t *testing.T) {
	p := pkgWith(FileContext{
		Path:    "a.md",
		Snippet: "...all **logging** goes through\n**internal/log** before...",
	})
	ev, ok := p.VerifyEvidence("a.md", "all logging goes through internal/log")
	if !ok {
		t.Fatal("markup and line breaks are retrieval's, not the quoter's — they must not fail a true quote")
	}
	if ev.Excerpt != "all logging goes through internal/log" {
		t.Errorf("Excerpt = %q, want the cleaned quote", ev.Excerpt)
	}
}

func TestVerifyEvidenceRejectsEmptyInput(t *testing.T) {
	p := pkgWith(FileContext{Path: "a.md", Snippet: "text"})
	if _, ok := p.VerifyEvidence("", "text"); ok {
		t.Error("empty document must not verify")
	}
	if _, ok := p.VerifyEvidence("a.md", "   "); ok {
		t.Error("empty excerpt must not verify")
	}
}

func TestVerifyEvidenceSearchesEverySection(t *testing.T) {
	p := ContextPackage{
		Rules: []FileContext{{Path: "rules/r.md", Repository: "engineering", Snippet: "the rule text"}},
		ADRs:  []FileContext{{Path: "ADR/0001.md", Repository: "engineering", Snippet: "the decision text"}},
	}
	if _, ok := p.VerifyEvidence("rules/r.md", "the rule text"); !ok {
		t.Error("evidence must verify against the Rules section")
	}
	if _, ok := p.VerifyEvidence("ADR/0001.md", "the decision text"); !ok {
		t.Error("evidence must verify against the ADRs section")
	}
}

func TestVerifyEvidenceNeverReturnsLowConfidence(t *testing.T) {
	p := pkgWith(FileContext{Path: "a.md", Snippet: "the text"})
	for _, excerpt := range []string{"the text", "the text and more around it", "unrelated"} {
		ev, ok := p.VerifyEvidence("a.md", excerpt)
		if ok && ev.Confidence == ConfidenceLow {
			t.Errorf("VerifyEvidence returned low confidence for %q — unverified evidence is not evidence", excerpt)
		}
	}
}

func TestSplitQualifiedLeavesOrdinaryPathsAlone(t *testing.T) {
	cases := map[string][2]string{
		"engineering:rules/a.md": {"engineering", "rules/a.md"},
		"rules/a.md":             {"", "rules/a.md"},
		":leading":               {"", ":leading"},
		"trailing:":              {"", "trailing:"},
	}
	for in, want := range cases {
		repo, path := splitQualified(in)
		if repo != want[0] || path != want[1] {
			t.Errorf("splitQualified(%q) = (%q, %q), want (%q, %q)", in, repo, path, want[0], want[1])
		}
	}
}
