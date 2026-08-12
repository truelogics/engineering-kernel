// Package chunker implements kernel.Chunker: splits a CanonicalDocument
// into Chunks — the unit Search actually indexes. No embeddings; three
// purely structural strategies (heading, paragraph, fixed-size), per
// Step 7's Milestone 7.
package chunker

import (
	"context"
	"fmt"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// Strategy selects how a document's Content is split into Chunks.
type Strategy string

const (
	// StrategyHeading splits on every heading line — a chunk is the
	// content between one heading and the next. Default strategy.
	StrategyHeading Strategy = "heading"
	// StrategyParagraph splits on blank lines, tagging each paragraph
	// with the nearest preceding heading.
	StrategyParagraph Strategy = "paragraph"
	// StrategyFixedSize splits into fixed-size rune windows, ignoring
	// document structure entirely.
	StrategyFixedSize Strategy = "fixed_size"
)

// defaultFixedSize is the window size (in runes) StrategyFixedSize uses
// when Chunker.FixedSize is unset.
const defaultFixedSize = 800

// Chunker implements kernel.Chunker.
type Chunker struct {
	Strategy Strategy
	// FixedSize is the target window size for StrategyFixedSize.
	// Defaults to defaultFixedSize when <= 0.
	FixedSize int
}

var _ kernel.Chunker = (*Chunker)(nil)

// New returns a Chunker using the given strategy. An empty Strategy
// behaves as StrategyHeading.
func New(strategy Strategy) *Chunker {
	return &Chunker{Strategy: strategy}
}

// Chunk implements kernel.Chunker.
func (c *Chunker) Chunk(ctx context.Context, doc domain.CanonicalDocument) ([]domain.Chunk, error) {
	switch c.Strategy {
	case StrategyParagraph:
		return c.chunkByParagraph(doc)
	case StrategyFixedSize:
		return c.chunkByFixedSize(doc)
	case StrategyHeading, "":
		return c.chunkByHeading(doc)
	default:
		return nil, fmt.Errorf("chunker: unknown strategy %q", c.Strategy)
	}
}

// chunkByHeading treats the span from one heading line to the next as one
// chunk — every heading, at any level, starts a new chunk. (Absorbing
// sub-headings into their parent section is a coarser strategy this
// doesn't implement; per RFC-0002/INTERFACES.md's open question on
// per-doc-type chunking strategies, that's a v2 refinement, not v1.)
func (c *Chunker) chunkByHeading(doc domain.CanonicalDocument) ([]domain.Chunk, error) {
	var chunks []domain.Chunk
	var heading string
	var buf []string
	index := 0

	flush := func() error {
		content := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = nil
		if content == "" {
			return nil
		}
		chunk, err := domain.NewChunk(doc.ID, index, heading, content)
		if err != nil {
			return err
		}
		chunks = append(chunks, chunk)
		index++
		return nil
	}

	for _, line := range strings.Split(doc.Content, "\n") {
		if text, ok := headingText(line); ok {
			if err := flush(); err != nil {
				return nil, err
			}
			heading = text
			continue
		}
		buf = append(buf, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// chunkByParagraph splits on blank lines; each chunk is tagged with the
// nearest preceding heading, so search results still show section context
// even at finer granularity than chunkByHeading.
func (c *Chunker) chunkByParagraph(doc domain.CanonicalDocument) ([]domain.Chunk, error) {
	var chunks []domain.Chunk
	var heading string
	var buf []string
	index := 0

	flush := func() error {
		content := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = nil
		if content == "" {
			return nil
		}
		chunk, err := domain.NewChunk(doc.ID, index, heading, content)
		if err != nil {
			return err
		}
		chunks = append(chunks, chunk)
		index++
		return nil
	}

	for _, line := range strings.Split(doc.Content, "\n") {
		if text, ok := headingText(line); ok {
			if err := flush(); err != nil {
				return nil, err
			}
			heading = text
			continue
		}
		if strings.TrimSpace(line) == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		buf = append(buf, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// chunkByFixedSize ignores structure entirely — non-overlapping windows
// of FixedSize runes (safe for multi-byte UTF-8 content).
func (c *Chunker) chunkByFixedSize(doc domain.CanonicalDocument) ([]domain.Chunk, error) {
	size := c.FixedSize
	if size <= 0 {
		size = defaultFixedSize
	}
	runes := []rune(strings.TrimSpace(doc.Content))
	if len(runes) == 0 {
		return nil, nil
	}

	var chunks []domain.Chunk
	index := 0
	for start := 0; start < len(runes); start += size {
		end := min(start+size, len(runes))
		content := strings.TrimSpace(string(runes[start:end]))
		if content == "" {
			continue
		}
		chunk, err := domain.NewChunk(doc.ID, index, "", content)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
		index++
	}
	return chunks, nil
}

// headingText reports whether line is an ATX-style markdown heading
// (`# Text` through `###### Text`) and, if so, its trimmed text.
func headingText(line string) (text string, ok bool) {
	trimmed := strings.TrimLeft(line, "#")
	level := len(line) - len(trimmed)
	if level == 0 || level > 6 {
		return "", false
	}
	if level < len(line) && line[level] != ' ' && line[level] != '\t' {
		return "", false // e.g. "##Not a heading" — CommonMark requires a space
	}
	text = strings.TrimSpace(trimmed)
	if text == "" {
		return "", false
	}
	return text, true
}
