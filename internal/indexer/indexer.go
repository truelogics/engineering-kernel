// Package indexer implements kernel.Indexer: orchestrates Collector ->
// Parser -> Normalizer -> graph.Extract -> Chunker per file, then writes
// through Storage. Owns incremental-index decisions; does not execute
// persistence mechanics itself or interpret file formats. See
// ARCHITECTURE.md and RFC-0003 (Sync, relationship extraction).
package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/graph"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// Indexer implements kernel.Indexer.
type Indexer struct {
	Collector kernel.Collector
	// Parsers are tried in order via CanParse; the first match parses
	// the RawDocument. v1 has one (markdown), but Indexer doesn't know
	// or care how many there are — a new Parser is just a new entry.
	Parsers    []kernel.Parser
	Normalizer kernel.Normalizer
	Chunker    kernel.Chunker
	Storage    kernel.Storage
	// Resolver extracts Relationships from a document's front-matter
	// references (RFC-0003). Nil skips extraction entirely — kept
	// optional so existing callers/tests that don't care about
	// relationships aren't forced to wire one up.
	Resolver kernel.ReferenceResolver
	// Now is injectable for deterministic tests; defaults to time.Now.
	Now func() time.Time
}

var _ kernel.Indexer = (*Indexer)(nil)

// New wires the pipeline components into an Indexer. resolver may be nil
// to skip relationship extraction (see Resolver's doc comment).
func New(collector kernel.Collector, parsers []kernel.Parser, normalizer kernel.Normalizer, chunker kernel.Chunker, storage kernel.Storage, resolver kernel.ReferenceResolver) *Indexer {
	return &Indexer{
		Collector:  collector,
		Parsers:    parsers,
		Normalizer: normalizer,
		Chunker:    chunker,
		Storage:    storage,
		Resolver:   resolver,
		Now:        time.Now,
	}
}

// Index implements kernel.Indexer: `eng index`'s entire pipeline for one
// Repository — a full walk, every time.
func (idx *Indexer) Index(ctx context.Context, repo domain.Repository) (kernel.IndexResult, error) {
	var result kernel.IndexResult

	raws, err := idx.Collector.Collect(ctx, repo)
	if err != nil {
		return result, fmt.Errorf("indexer: collect %s: %w", repo.Name, err)
	}
	result.Scanned = len(raws)

	// Once per run, not per document: the repository's statement about
	// its own directories does not change mid-index (RFC-0007).
	taxonomy, err := loadTaxonomy(repo)
	if err != nil {
		return result, err
	}

	for _, raw := range raws {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		idx.indexOne(ctx, repo, taxonomy, raw, &result)
	}

	if err := idx.finish(ctx, &repo, result, true); err != nil {
		return result, err
	}
	return result, nil
}

// Sync implements kernel.Indexer: incremental re-index using git as the
// source of truth for what changed since repo.LastIndexedCommit,
// including removing rows for deleted files. Falls back to a full Index
// when the Collector doesn't support incremental collection, or repo has
// never been indexed. See RFC-0003/GRAPH.md.
func (idx *Indexer) Sync(ctx context.Context, repo domain.Repository) (kernel.IndexResult, error) {
	ic, ok := idx.Collector.(kernel.IncrementalCollector)
	if !ok || repo.LastIndexedCommit == "" {
		return idx.Index(ctx, repo)
	}

	var result kernel.IndexResult
	changed, deletedPaths, err := ic.CollectChanged(ctx, repo, repo.LastIndexedCommit)
	if err != nil {
		return result, fmt.Errorf("indexer: collect changed for %s: %w", repo.Name, err)
	}
	result.Scanned = len(changed)

	taxonomy, err := loadTaxonomy(repo)
	if err != nil {
		return result, err
	}

	for _, raw := range changed {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		idx.indexOne(ctx, repo, taxonomy, raw, &result)
	}

	for _, path := range deletedPaths {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		docID := domain.DocumentID(repo.ID, path)
		if err := idx.Storage.DeleteDocument(ctx, docID); err != nil {
			result.Fail(path, "delete: "+err.Error())
			continue
		}
		result.Deleted++
	}

	if err := idx.finish(ctx, &repo, result, false); err != nil {
		return result, err
	}
	return result, nil
}

// indexOne runs one RawDocument through Parser -> Normalizer -> (skip if
// unchanged) -> graph.Extract -> Chunker -> Storage, updating result as
// it goes. Errors at any stage count against result.Errors and move on
// to the next file — one bad file must not abort the whole run.
func (idx *Indexer) indexOne(ctx context.Context, repo domain.Repository, taxonomy domain.Taxonomy, raw domain.RawDocument, result *kernel.IndexResult) {
	parser := idx.pickParser(raw)
	if parser == nil {
		result.Fail(raw.Path, "no parser handles this file type")
		return
	}

	doc, err := parser.Parse(ctx, raw)
	if err != nil {
		result.Fail(raw.Path, "parse: "+err.Error())
		return
	}

	doc, err = idx.Normalizer.Normalize(ctx, doc)
	if err != nil {
		result.Fail(raw.Path, "normalize: "+err.Error())
		return
	}

	// After classification, before storage: fills in what the kernel's
	// own vocabulary could not name, and never overrules it.
	applyTaxonomy(taxonomy, &doc)

	existing, found, err := idx.Storage.FindDocumentByPath(ctx, repo.ID, doc.Path)
	if err != nil {
		result.Fail(raw.Path, "lookup: "+err.Error())
		return
	}
	// Unchanged means unchanged *as indexed*, not merely byte-identical.
	// Classification is an input too: adding a .engineering.yaml to a
	// repository changes no markdown file's bytes, so a content-hash-only
	// check skipped every existing row and left it `unknown` — which made
	// RFC-0007 work on a fresh index and do nothing on the upgrade path
	// every real adopter is on. Found reviewing that change before it
	// merged.
	if found && doc.ContentHash != "" && existing.ContentHash == doc.ContentHash && existing.Type == doc.Type {
		result.Unchanged++
		return
	}

	if idx.Resolver != nil {
		rels, err := graph.Extract(ctx, doc, idx.Resolver)
		if err != nil {
			result.Fail(raw.Path, "link extraction: "+err.Error())
			return
		}
		doc.Relationships = append(doc.Relationships, rels...)
	}

	chunks, err := idx.Chunker.Chunk(ctx, doc)
	if err != nil {
		result.Fail(raw.Path, "chunk: "+err.Error())
		return
	}

	if err := idx.Storage.PutDocument(ctx, doc); err != nil {
		result.Fail(raw.Path, "write document: "+err.Error())
		return
	}
	if err := idx.Storage.PutChunks(ctx, doc.ID, chunks); err != nil {
		result.Fail(raw.Path, "write chunks: "+err.Error())
		return
	}

	if found {
		result.Updated++
	} else {
		result.Added++
	}
}

func (idx *Indexer) pickParser(raw domain.RawDocument) kernel.Parser {
	for _, p := range idx.Parsers {
		if p.CanParse(raw) {
			return p
		}
	}
	return nil
}

// finish records the repository's new LastIndexedCommit (if the
// Collector supports it) and updates IndexState, after either an Index
// or a Sync run.
func (idx *Indexer) finish(ctx context.Context, repo *domain.Repository, result kernel.IndexResult, full bool) error {
	if err := idx.recordIndexedCommit(ctx, repo); err != nil {
		return fmt.Errorf("indexer: record indexed commit for %s: %w", repo.Name, err)
	}
	if err := idx.updateIndexState(ctx, *repo, result, full); err != nil {
		return fmt.Errorf("indexer: update index state for %s: %w", repo.Name, err)
	}
	return nil
}

func (idx *Indexer) recordIndexedCommit(ctx context.Context, repo *domain.Repository) error {
	ic, ok := idx.Collector.(kernel.IncrementalCollector)
	if !ok {
		return nil
	}
	ref, err := ic.CurrentRef(ctx, *repo)
	if err != nil || ref == "" {
		return nil // best-effort; not a git repo is fine
	}
	*repo = repo.MarkIndexed(ref, idx.now())
	return idx.Storage.PutRepository(ctx, *repo)
}

// updateIndexState queries the actual current document count from
// Storage rather than deriving it from this run's tallies — correct for
// both a full Index and a partial Sync, and doesn't undercount when
// Sync's DeleteDocument calls have already changed what's really there.
func (idx *Indexer) updateIndexState(ctx context.Context, repo domain.Repository, result kernel.IndexResult, full bool) error {
	docs, err := idx.Storage.ListDocuments(ctx, repo.ID)
	if err != nil {
		return err
	}

	existing, _, err := idx.Storage.GetIndexState(ctx, repo.ID)
	if err != nil {
		return err
	}

	status := kernel.IndexStatusClean
	if result.Errors > 0 {
		status = kernel.IndexStatusError
	}
	state := kernel.IndexState{
		RepositoryID:           repo.ID,
		DocumentCount:          len(docs),
		LastFullIndexAt:        existing.LastFullIndexAt,
		LastIncrementalIndexAt: existing.LastIncrementalIndexAt,
		Status:                 status,
	}
	now := idx.now()
	if full {
		state.LastFullIndexAt = now
	} else {
		state.LastIncrementalIndexAt = now
	}
	return idx.Storage.PutIndexState(ctx, state)
}

func (idx *Indexer) now() time.Time {
	if idx.Now != nil {
		return idx.Now()
	}
	return time.Now()
}
