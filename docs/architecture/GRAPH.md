---
doc: GRAPH
audience: [human, agent]
status: draft
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Graph & Incremental Indexing

> Companion to [RFC-0003](../../rfcs/0003-engineering-intelligence.md) (why
> this exists), [INTERFACES.md](INTERFACES.md), and
> [DATABASE.md](DATABASE.md). Concrete design for `internal/graph` and
> `eng sync` — the same "design only, no methods finalized as code" stance
> INTERFACES.md took before Step 7. **Nothing here is implemented yet.**

## Scope

Populates and traverses Document↔Document `references`/`supersedes`
edges, and adds incremental (git-diff-based) indexing. Does **not** add
Component/Service/Issue/Person as real entities, embeddings, or a second
storage engine — see RFC-0003's non-goals.

## Relationship extraction

### What gets extracted

| Front-matter field | Relationship type | Resolution |
|---|---|---|
| `supersedes: NNNN` | `supersedes` (this doc → the referenced one) | Convention: glob `NNNN-*.md` in the same directory |
| `superseded_by: NNNN` | `supersedes` (referenced doc → this one — same edge, other direction, not a second row) | Same as above |
| A reference that already looks like a path (`docs/architecture/X.md`) | `references` | Direct: `contentID(repositoryID, path)` |

Everything else stays as Metadata/Tags, unresolved — extraction only
produces a `Relationship` when the target can actually be resolved to a
real `documents.id`. An unresolvable reference (typo, moved file) is
silently skipped, not an error — see Open questions on whether that
should surface somewhere (e.g. `eng doctor`, not designed yet either).

### Where it runs

```
Indexer
  Collector → Parser → Normalizer → graph.Extract → Chunker → Storage
```

`graph.Extract(ctx, doc domain.CanonicalDocument, siblingPaths []string)
[]domain.Relationship` — takes the normalized document plus the set of
other paths in its repository (needed to resolve the `NNNN-*.md`
convention), returns any Relationships it could resolve. `Indexer` appends
these to `doc.Relationships` before calling `Storage.PutDocument`, which
already knows how to persist a Document's Relationships (Step 7,
unchanged).

## Traversal

```go
// Sketch — not final method signatures, per RFC-0003's design-first stance.
type Graph interface {
    Neighbors(ctx context.Context, documentID string) ([]domain.Relationship, error)
    Traverse(ctx context.Context, documentID string, depth int) (Subgraph, error)
}

type Subgraph struct {
    Root  string
    Edges []domain.Relationship
}
```

`Traverse`'s implementation is a `WITH RECURSIVE` query against
`relationships`, bounded by `depth` (default: 2) — not a new storage
engine, not an in-memory graph library. Something like:

```sql
WITH RECURSIVE walk(id, depth) AS (
  SELECT ?, 0
  UNION
  SELECT CASE WHEN r.from_document_id = w.id THEN r.to_document_id ELSE r.from_document_id END,
         w.depth + 1
  FROM relationships r
  JOIN walk w ON (r.from_document_id = w.id OR r.to_document_id = w.id)
  WHERE w.depth < ?
)
SELECT DISTINCT id FROM walk WHERE id != ?;
```

## Incremental indexing

### `IncrementalCollector` (optional interface, `internal/kernel`)

```go
// Sketch.
type IncrementalCollector interface {
    // CollectChanged returns RawDocuments changed since sinceCommit,
    // plus paths deleted since then. sinceCommit is Repository.LastIndexedCommit.
    CollectChanged(ctx context.Context, repo domain.Repository, sinceCommit string) (changed []domain.RawDocument, deleted []string, err error)
}
```

`internal/collector/filesystem` implements this via:

1. `git diff --name-status <sinceCommit>...HEAD -- '*.md' '*.markdown'`
   for committed changes.
2. `git status --porcelain -- '*.md' '*.markdown'` for working-tree
   changes (staged and unstaged) — per RFC-0003's Trade-offs, `eng sync`
   must not silently ignore uncommitted edits.
3. Union the two, classify each path Added/Modified/Deleted, read bytes
   for everything except Deleted.

### `Indexer.Sync`

A second method alongside `Index` (not a flag on `Index` — a distinct
operation per RFC-0003's Alternatives):

```go
// Sketch, extends the existing kernel.Indexer interface.
type Indexer interface {
    Index(ctx context.Context, repo domain.Repository) (IndexResult, error)
    Sync(ctx context.Context, repo domain.Repository) (IndexResult, error)
}
```

`Sync` type-asserts its `Collector` to `IncrementalCollector`; if it
doesn't implement it, or `repo.LastIndexedCommit` is empty, `Sync` just
calls `Index`. Otherwise: run `CollectChanged`, feed `changed` through
the normal Parser→Normalizer→graph.Extract→Chunker→Storage path, and for
each `deleted` path, remove its `documents`/`document_chunks`/`tags`/
`relationships`/FTS rows (a new `Storage.DeleteDocument(ctx, id) error`
method — the first hard-delete this kernel needs; Step 7 never removed a
row).

### `kernel.IndexResult` grows one field

```go
type IndexResult struct {
    Scanned, Added, Updated, Unchanged, Errors int
    Deleted int // new — only ever non-zero from Sync
}
```

Per RFC-0003's Open questions, this RFC leans toward growing the existing
type rather than introducing a second result shape for `Sync`.

## CLI surface

`eng sync [path]` — an eighth command alongside the seven in
[`CLI.md`](../cli/CLI.md). Same shape as `eng index`'s output line, with
a `deleted` count:

```
$ eng sync
engineering-kernel: 3 scanned, 1 added, 2 updated, 0 unchanged, 1 deleted, 0 errors
```

## Open questions

- Should an unresolvable reference (bad `supersedes:` value) surface
  anywhere, or stay silent? `eng doctor` (designed, not implemented) is
  the natural home if it should.
- Does `graph.Extract` need repository-wide sibling-path context (to
  resolve `NNNN-*.md`), or should the convention resolver work off
  `Storage.ListDocuments` instead of a passed-in list — avoiding another
  parameter Indexer has to assemble?
- `Storage.DeleteDocument` is the first hard-delete in this schema —
  does it need to cascade explicitly (chunks, tags, relationships, FTS
  rows) in application code, or can `ON DELETE CASCADE` foreign keys
  handle it now that a real delete path exists? DATABASE.md's current
  schema has no `ON DELETE` clauses at all.
