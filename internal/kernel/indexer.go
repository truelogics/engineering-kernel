package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// IndexResult summarizes one Index or Sync run — the numbers `eng
// index`/`eng sync` print.
type IndexResult struct {
	Scanned   int
	Added     int
	Updated   int
	Unchanged int
	Errors    int
	// Failures says which files failed and why. Errors is its length.
	//
	// The count used to be all a caller got, and it is unactionable:
	// "1 errors" against a 727-file repository names no file and no
	// cause. Twice now that has cost real time — four rule files
	// silently unindexed by invalid front matter in Sprint 7, and an
	// unexplained failure while indexing the first outside project in
	// Sprint 12.
	Failures []IndexFailure
	// Deleted is only ever non-zero from Sync — Index never removes rows
	// for files no longer on disk. See RFC-0003.
	Deleted int
}

// IndexFailure is one file the indexer could not process, and why.
type IndexFailure struct {
	Path   string
	Reason string
}

// Fail records a failure and increments Errors, keeping the two in step.
func (r *IndexResult) Fail(path, reason string) {
	r.Errors++
	r.Failures = append(r.Failures, IndexFailure{Path: path, Reason: reason})
}

// Indexer orchestrates one `eng index`/`eng sync` run for a Repository:
// Collector -> Parser -> Normalizer -> Chunker per item, then writes via
// Storage. Owns incremental-index decisions (skipping unchanged files via
// ContentHash). Must not execute persistence mechanics directly
// (Storage's job) or interpret file formats itself (Parser's job).
type Indexer interface {
	Index(ctx context.Context, repo domain.Repository) (IndexResult, error)
	// Sync incrementally re-indexes repo using git as the source of
	// truth for what changed — RFC-0003/GRAPH.md. Falls back to Index
	// when the underlying Collector doesn't support incremental
	// collection or repo has never been indexed.
	Sync(ctx context.Context, repo domain.Repository) (IndexResult, error)
}
