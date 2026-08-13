// Package search implements kernel.Search: ranked full-text query,
// blended with graph and (optionally) embedding signals, plus
// related-document enrichment, on top of kernel.Storage's raw FTS5
// results. See ARCHITECTURE.md's Search component and RFC-0003 (hybrid
// search, Milestone 5). The kernel.Search interface itself doesn't
// change — Storage's schema and ARCHITECTURE.md's "Where later
// milestones attach" both said results would just get better, not that
// the seam would.
package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/graph"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// maxRelated caps how many related documents Search attaches per result,
// so one heavily-tagged document doesn't drown out the actual match.
const maxRelated = 5

// candidatePoolMultiplier and minCandidatePool control how many raw
// keyword hits get pulled before re-ranking and truncating to the
// requested limit — hybrid ranking needs a wider pool than the final
// result count to have anything to blend.
const (
	candidatePoolMultiplier = 3
	minCandidatePool        = 20
)

// statsTagKeys are the structural Tags internal/parser/markdown attaches
// to nearly every document (heading_count, etc.) — excluded from the
// shared-tag "related" fallback because they'd make almost every document
// "related" to every other one, which isn't useful.
var statsTagKeys = map[string]bool{
	"heading_count":    true,
	"code_block_count": true,
	"link_count":       true,
	"table_count":      true,
}

// Search implements kernel.Search against a kernel.Storage, blended with
// optional Graph and Embeddings signals — both nil-safe, degrading to
// pure keyword ranking when unset.
type Search struct {
	Storage kernel.Storage
	// Graph enables the graph-connectivity boost. Nil disables it (score
	// contribution 0), not an error — see rank.go's graphBoosts.
	Graph *graph.Graph
	// Embeddings enables the semantic-similarity signal. Nil disables it
	// entirely (Milestone 4 shipped no provider, so this is nil in every
	// real deployment today) — see rank.go's embeddingBoosts.
	Embeddings kernel.EmbeddingProvider
	// Weights overrides the keyword/graph/embedding blend (Milestone 7).
	// Zero value uses Milestone 5's defaults — see rank.go's
	// defaultWeights.
	Weights RankWeights
}

var _ kernel.Search = (*Search)(nil)

// UserQuery turns raw human search text into an FTS5 query that cannot
// be a syntax error.
//
// FTS5's query grammar treats several ordinary characters as operators:
// `a-b` parses as a column filter, so searching for a hyphenated name
// fails with "no such column". Real failure, found by a consumer
// searching for this organization's own repository names
// (engineering-mcp/KERNEL_REQUIREMENTS.md #1):
//
//	Search("engineering-mcp kernel policy")
//	  → SQL logic error: no such column: mcp
//
// Each whitespace-separated term is wrapped as an FTS5 phrase, which
// sidesteps query-syntax parsing entirely; the content tokenizer still
// splits a quoted phrase the way it would a bareword, so matching for
// ordinary words is unchanged. Terms are joined with spaces, preserving
// FTS5's implicit AND — this fixes a crash, it does not retune search.
//
// Text with nothing searchable in it returns ok false. There is no FTS5
// query meaning "match nothing", and an empty MATCH is itself a syntax
// error — so the caller returns no results rather than the function
// returning something that would reintroduce the failure it exists to
// prevent.
//
// Callers passing a *constructed* FTS expression must not use this:
// re-quoting `"a" OR "b"` would turn its operator into a search term.
// The retriever builds its own expression and calls Search directly
// (see retriever.keywords).
func UserQuery(text string) (query string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false
	}
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " "), true
}

// New returns a Search backed by storage, with no graph or embedding
// signal configured — equivalent to Step 7's keyword-only Search. Use the
// struct literal directly to configure Graph/Embeddings.
func New(storage kernel.Storage) *Search {
	return &Search{Storage: storage}
}

// Search implements kernel.Search: fetches a wider candidate pool than
// requested, blends keyword/graph/embedding signals via rank, then
// truncates to opts.Limit.
func (s *Search) Search(ctx context.Context, query string, opts kernel.SearchOptions) ([]kernel.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	poolOpts := opts
	poolOpts.Limit = limit * candidatePoolMultiplier
	if poolOpts.Limit < minCandidatePool {
		poolOpts.Limit = minCandidatePool
	}

	matches, err := s.Storage.SearchChunks(ctx, query, poolOpts)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(matches) == 0 {
		return nil, nil
	}

	scored, err := s.rank(ctx, query, matches)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].blended > scored[j].blended })

	// One row per document, keeping its best-scoring passage.
	//
	// Chunks are what FTS matches, and a long document matches on several
	// of them — so without this the same path appears repeatedly. That is
	// untidy in `eng search` and actively harmful here: the collapse
	// happens *before* the limit is applied, because trimming first spends
	// result slots on repeats and pushes genuinely different documents off
	// the end. Measured on a 745-document repository, one query returned
	// 18 rows covering 15 distinct documents — three slots wasted, three
	// documents never seen.
	//
	// This is also the behaviour already promised to clients:
	// engineering-mcp's /review-branch command tells the model that "only
	// the top-scoring passage per document is kept", and reasons about
	// verify_evidence on that basis.
	scored = collapseToDocuments(scored)
	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]kernel.SearchResult, 0, len(scored))
	for _, sc := range scored {
		related, err := s.relatedDocuments(ctx, sc.match.Document)
		if err != nil {
			return nil, err
		}
		results = append(results, kernel.SearchResult{
			Document: sc.match.Document,
			Chunk:    sc.match.Chunk,
			Score:    sc.blended,
			Snippet:  sc.match.Snippet,
			Related:  related,
		})
	}
	return results, nil
}

// collapseToDocuments keeps the first occurrence of each document.
//
// Order-preserving, and it relies on the caller having sorted by score
// already: "first" is therefore "best". Written this way rather than as a
// map-then-rebuild so a document's position is decided by its best
// passage and nothing else — a document must not move because it happened
// to match twice.
func collapseToDocuments(scored []scoredMatch) []scoredMatch {
	seen := make(map[string]bool, len(scored))
	out := scored[:0:0]
	for _, sc := range scored {
		id := sc.match.Document.ID
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, sc)
	}
	return out
}

// relatedDocuments finds other documents connected to doc: explicit
// Relationships first (an ADR's `supersedes:`, etc.); if there are none —
// the common case in v1, since nothing populates Relationships except a
// front-matter reference that resolves — falls back to documents sharing
// a non-structural Tag (e.g. `audience: agent`).
func (s *Search) relatedDocuments(ctx context.Context, doc domain.CanonicalDocument) ([]domain.CanonicalDocument, error) {
	rels, err := s.Storage.ListRelationships(ctx, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("search: related documents for %s: %w", doc.ID, err)
	}

	seen := map[string]bool{doc.ID: true}
	var related []domain.CanonicalDocument
	for _, rel := range rels {
		otherID := rel.ToDocumentID
		if otherID == doc.ID {
			otherID = rel.FromDocumentID
		}
		if seen[otherID] {
			continue
		}
		seen[otherID] = true
		if other, err := s.Storage.GetDocument(ctx, otherID); err == nil {
			related = append(related, other)
		}
	}
	if len(related) > 0 {
		return related, nil
	}

	for _, tag := range doc.Tags {
		if statsTagKeys[tag.Key] {
			continue
		}
		others, err := s.Storage.FindDocumentsByTag(ctx, doc.RepositoryID, tag.Key, tag.Value, doc.ID)
		if err != nil {
			return nil, fmt.Errorf("search: related documents for %s: %w", doc.ID, err)
		}
		for _, other := range others {
			if seen[other.ID] {
				continue
			}
			seen[other.ID] = true
			related = append(related, other)
			if len(related) >= maxRelated {
				return related, nil
			}
		}
	}
	return related, nil
}
