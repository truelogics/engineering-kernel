package markdown

import (
	"context"
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
)

func tagValue(t *testing.T, tags []domain.Tag, key string) (string, bool) {
	t.Helper()
	for _, tg := range tags {
		if tg.Key == key {
			return tg.Value, true
		}
	}
	return "", false
}

func TestCanParse(t *testing.T) {
	p := New()
	cases := map[string]bool{
		"README.md":      true,
		"notes.markdown": true,
		"main.go":        false,
		"data.yaml":      false,
	}
	for path, want := range cases {
		raw := domain.RawDocument{Path: path}
		if got := p.CanParse(raw); got != want {
			t.Errorf("CanParse(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseFrontMatterAndBody(t *testing.T) {
	content := `---
doc: README
audience: [human, agent]
status: living
owner: ai-memory
---

# AI Memory

## What is this?

The kernel of the AI Engineering OS.

` + "```go\nfmt.Println(\"hi\")\n```" + `

See [ARCHITECTURE.md](docs/architecture/ARCHITECTURE.md) for more.

| A | B |
|---|---|
| 1 | 2 |
`
	raw, err := domain.NewRawDocument("repo-1", "README.md", []byte(content))
	if err != nil {
		t.Fatalf("NewRawDocument: %v", err)
	}

	doc, err := New().Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if doc.Title != "AI Memory" {
		t.Errorf("Title = %q, want %q", doc.Title, "AI Memory")
	}
	if doc.Type != domain.DocTypeReadme {
		t.Errorf("Type = %q, want %q", doc.Type, domain.DocTypeReadme)
	}
	if v, ok := doc.Metadata.Get("status"); !ok || v != "living" {
		t.Errorf("Metadata[status] = (%q, %v), want (%q, true)", v, ok, "living")
	}
	if v, ok := doc.Metadata.Get("owner"); !ok || v != "ai-memory" {
		t.Errorf("Metadata[owner] = (%q, %v), want (%q, true)", v, ok, "ai-memory")
	}
	if _, ok := doc.Metadata.Get("audience"); ok {
		t.Error("Metadata[audience] should not be set — list fields become Tags, not Metadata")
	}

	human, ok := tagValue(t, doc.Tags, "audience")
	if !ok || (human != "human" && human != "agent") {
		t.Errorf("expected an 'audience' tag with value human or agent, got tags: %+v", doc.Tags)
	}
	audienceCount := 0
	for _, tg := range doc.Tags {
		if tg.Key == "audience" {
			audienceCount++
		}
	}
	if audienceCount != 2 {
		t.Errorf("expected 2 'audience' tags (human, agent), got %d", audienceCount)
	}

	for _, want := range []string{"heading_count", "code_block_count", "link_count", "table_count"} {
		if v, ok := tagValue(t, doc.Tags, want); !ok || v == "0" {
			t.Errorf("expected non-zero tag %q, got (%q, %v)", want, v, ok)
		}
	}

	if !strings.Contains(doc.Content, "```go") {
		t.Error("Content should preserve the raw markdown body, including fenced code blocks")
	}
	if strings.Contains(doc.Content, "---\ndoc: README") {
		t.Error("Content should not include front-matter")
	}
	if doc.ContentHash == "" {
		t.Error("expected ContentHash to be set")
	}
}

func TestParseWithoutFrontMatter(t *testing.T) {
	raw, err := domain.NewRawDocument("repo-1", "notes/random.md", []byte("Just a paragraph, no heading."))
	if err != nil {
		t.Fatalf("NewRawDocument: %v", err)
	}
	doc, err := New().Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if doc.Title != raw.Path {
		t.Errorf("Title = %q, want fallback to path %q", doc.Title, raw.Path)
	}
	if doc.Type != domain.DocTypeUnknown {
		t.Errorf("Type = %q, want %q", doc.Type, domain.DocTypeUnknown)
	}
	if len(doc.Tags) != 0 {
		t.Errorf("expected no tags for a plain paragraph with no headings/code/links/tables, got %+v", doc.Tags)
	}
}

func TestParseRejectsInvalidFrontMatter(t *testing.T) {
	content := "---\nthis: [is not: valid: yaml\n---\nbody"
	raw, err := domain.NewRawDocument("repo-1", "broken.md", []byte(content))
	if err != nil {
		t.Fatalf("NewRawDocument: %v", err)
	}
	if _, err := New().Parse(context.Background(), raw); err == nil {
		t.Fatal("Parse: expected error for invalid front-matter YAML")
	}
}

func TestInferDocTypeByPathConvention(t *testing.T) {
	cases := map[string]domain.DocType{
		"README.md":                        domain.DocTypeReadme,
		"rfcs/0001-engineering-kernel.md":  domain.DocTypeRFC,
		"engineering/ADR/0007-decision.md": domain.DocTypeADR,
		"engineering/rules/no-secrets.md":  domain.DocTypeRule,
		"roadmap/NOW.md":                   domain.DocTypeRoadmap,
		"docs/architecture/UNRELATED.md":   domain.DocTypeUnknown,
	}
	for path, want := range cases {
		got := inferDocType(path, domain.NewMetadata())
		if got != want {
			t.Errorf("inferDocType(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestInferDocTypeDirectoryOutranksFileName guards a real retrieval
// failure: `base == "readme.md"` was checked before the directory
// segments, so `rules/README.md` — the index naming everything in
// `rules/` — classified as a generic readme and could never be returned
// as a rule. Six AI Review runs across three repositories retrieved zero
// rules partly because of this.
func TestInferDocTypeDirectoryOutranksFileName(t *testing.T) {
	cases := map[string]domain.DocType{
		"rules/README.md":           domain.DocTypeRule,
		"engineering/ADR/README.md": domain.DocTypeADR,
		"rfcs/README.md":            domain.DocTypeRFC,
		"README.md":                 domain.DocTypeReadme,
		"docs/README.md":            domain.DocTypeReadme,
	}
	for path, want := range cases {
		if got := inferDocType(path, domain.NewMetadata()); got != want {
			t.Errorf("inferDocType(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestInferDocTypeFromFrontMatter covers the `doc:` values a rule or ADR
// declares explicitly, which outrank path convention — a rule stored
// outside a rules/ directory is still a rule.
func TestInferDocTypeFromFrontMatter(t *testing.T) {
	cases := []struct {
		doc  string
		want domain.DocType
	}{
		{"RULE", domain.DocTypeRule},
		{"ADR", domain.DocTypeADR},
		{"RFC", domain.DocTypeRFC},
		{"ARCHITECTURE", domain.DocTypeStandard},
	}
	for _, c := range cases {
		meta := domain.NewMetadata()
		meta.Set("doc", c.doc)
		if got := inferDocType("some/unrelated/path.md", meta); got != c.want {
			t.Errorf("inferDocType(doc: %s) = %q, want %q", c.doc, got, c.want)
		}
	}
}

func TestParseIsDeterministicForSameInput(t *testing.T) {
	raw, _ := domain.NewRawDocument("repo-1", "README.md", []byte("# Hi\n\nHello."))
	a, err := New().Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b, err := New().Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if a.ID != b.ID || a.ContentHash != b.ContentHash {
		t.Fatalf("Parse not deterministic: (%q, %q) != (%q, %q)", a.ID, a.ContentHash, b.ID, b.ContentHash)
	}
}

func TestParsePreservesNumericFrontMatterFormatting(t *testing.T) {
	content := "---\nsupersedes: 0001\ncount: 007\n---\n\nBody.\n"
	raw, err := domain.NewRawDocument("repo-1", "rfcs/0002-x.md", []byte(content))
	if err != nil {
		t.Fatalf("NewRawDocument: %v", err)
	}
	doc, err := New().Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if v, ok := doc.Metadata.Get("supersedes"); !ok || v != "0001" {
		t.Fatalf("Metadata[supersedes] = (%q, %v), want (%q, true) — leading zero must survive YAML's numeric resolution", v, ok, "0001")
	}
	if v, ok := doc.Metadata.Get("count"); !ok || v != "007" {
		t.Fatalf("Metadata[count] = (%q, %v), want (%q, true)", v, ok, "007")
	}
}
