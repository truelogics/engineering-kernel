package chunker

import (
	"context"
	"strings"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

func testDoc(t *testing.T, content string) domain.CanonicalDocument {
	t.Helper()
	doc, err := domain.NewCanonicalDocument("repo-1", "src-1", "README.md")
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Content = content
	return doc
}

func TestChunkByHeadingSplitsOnEachHeading(t *testing.T) {
	content := "Intro text.\n\n# Section One\nBody one.\n\n## Subsection\nNested body.\n\n# Section Two\nBody two.\n"
	doc := testDoc(t, content)

	chunks, err := New(StrategyHeading).Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: unexpected error: %v", err)
	}
	// Every heading line, at any level, starts a new chunk: intro,
	// Section One, Subsection, Section Two.
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "" {
		t.Errorf("chunk 0 Heading = %q, want empty (before any heading)", chunks[0].Heading)
	}
	if !strings.Contains(chunks[0].Content, "Intro text.") {
		t.Errorf("chunk 0 Content = %q, want to contain intro text", chunks[0].Content)
	}
	if chunks[1].Heading != "Section One" || !strings.Contains(chunks[1].Content, "Body one.") {
		t.Errorf("chunk 1 = %+v, want Heading %q with %q", chunks[1], "Section One", "Body one.")
	}
	if chunks[2].Heading != "Subsection" || !strings.Contains(chunks[2].Content, "Nested body.") {
		t.Errorf("chunk 2 = %+v, want Heading %q with %q", chunks[2], "Subsection", "Nested body.")
	}
	if chunks[3].Heading != "Section Two" {
		t.Errorf("chunk 3 Heading = %q, want %q", chunks[3].Heading, "Section Two")
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk %d Index = %d, want %d", i, c.Index, i)
		}
		if c.DocumentID != chunks[0].DocumentID {
			t.Errorf("chunk %d DocumentID inconsistent: %q", i, c.DocumentID)
		}
	}
}

func TestChunkByHeadingIgnoresMalformedHeadings(t *testing.T) {
	content := "##NotAHeading because no space\n\nActual paragraph.\n"
	doc := testDoc(t, content)
	chunks, err := New(StrategyHeading).Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1 (no real heading present): %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "##NotAHeading") {
		t.Errorf("malformed heading line should be treated as body text, got: %q", chunks[0].Content)
	}
}

func TestChunkByParagraphSplitsOnBlankLinesAndTracksHeading(t *testing.T) {
	content := "# Title\nFirst paragraph.\n\nSecond paragraph.\n\n# Next\nThird paragraph.\n"
	doc := testDoc(t, content)

	chunks, err := New(StrategyParagraph).Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: unexpected error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "Title" || chunks[1].Heading != "Title" {
		t.Errorf("expected first two paragraphs tagged with heading %q, got %q and %q", "Title", chunks[0].Heading, chunks[1].Heading)
	}
	if chunks[2].Heading != "Next" {
		t.Errorf("chunk 2 Heading = %q, want %q", chunks[2].Heading, "Next")
	}
}

func TestChunkByFixedSizeSplitsIntoWindows(t *testing.T) {
	content := strings.Repeat("a", 25)
	doc := testDoc(t, content)

	c := &Chunker{Strategy: StrategyFixedSize, FixedSize: 10}
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: unexpected error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3 (10+10+5): %+v", len(chunks), chunks)
	}
	if len(chunks[0].Content) != 10 || len(chunks[1].Content) != 10 || len(chunks[2].Content) != 5 {
		t.Fatalf("unexpected chunk sizes: %d, %d, %d", len(chunks[0].Content), len(chunks[1].Content), len(chunks[2].Content))
	}
}

func TestChunkByFixedSizeUsesDefaultWhenUnset(t *testing.T) {
	content := strings.Repeat("a", defaultFixedSize+1)
	doc := testDoc(t, content)

	chunks, err := New(StrategyFixedSize).Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: unexpected error: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
}

func TestChunkEmptyContentReturnsNoChunks(t *testing.T) {
	for _, strategy := range []Strategy{StrategyHeading, StrategyParagraph, StrategyFixedSize} {
		doc := testDoc(t, "   \n\n  ")
		chunks, err := New(strategy).Chunk(context.Background(), doc)
		if err != nil {
			t.Fatalf("[%s] Chunk: unexpected error: %v", strategy, err)
		}
		if len(chunks) != 0 {
			t.Errorf("[%s] got %d chunks for blank content, want 0", strategy, len(chunks))
		}
	}
}

func TestChunkRejectsUnknownStrategy(t *testing.T) {
	doc := testDoc(t, "content")
	if _, err := New("bogus").Chunk(context.Background(), doc); err == nil {
		t.Fatal("Chunk: expected error for unknown strategy")
	}
}

func TestDefaultStrategyIsHeading(t *testing.T) {
	doc := testDoc(t, "# Title\nBody.")
	c := &Chunker{} // zero value, no Strategy set
	chunks, err := c.Chunk(context.Background(), doc)
	if err != nil {
		t.Fatalf("Chunk: unexpected error: %v", err)
	}
	if len(chunks) != 1 || chunks[0].Heading != "Title" {
		t.Fatalf("zero-value Chunker should default to heading strategy, got: %+v", chunks)
	}
}
