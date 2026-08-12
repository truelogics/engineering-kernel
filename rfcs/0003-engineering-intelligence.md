---
rfc: 0003
title: "Engineering Intelligence: Relationships, Graph, Incremental Sync"
status: draft
author: founding-team
created: 2026-08-02
supersedes:
superseded_by:
resulting_adr:
---

# RFC 0003 — Engineering Intelligence: Relationships, Graph, Incremental Sync

## Summary

Scopes the first slice of Step 8 ("Engineering Intelligence"): a
relationship-extraction and graph-traversal layer (`internal/graph`), and
incremental indexing (`eng sync`). Explicitly excludes embedding
providers, hybrid search, a Context Builder implementation, ranking,
benchmarks, a plugin system, and a public HTTP API — those remain later
RFCs, scoped only once this slice is real and something actually consumes
it. This RFC is design only, per the same discipline RFC-0001/RFC-0002
followed before Step 7 wrote any code.

## Motivation

Today, [`internal/search`](../internal/search/)'s "related documents" is a
thin fallback: explicit Relationships if any exist, else documents sharing
a non-structural Tag. Genuinely useful, but not what "Review PR #245"
needs — real traversal (this ADR → what implements it → what depends on
it). Building that requires two things that don't exist yet:

1. **Relationships that actually get populated.** RFC-0001 explicitly
   deferred auto-populating them from front-matter references (e.g. an
   ADR's `supersedes:`); nothing else creates them either. Today, almost
   every document's `Relationships` slice is empty.
2. **Indexing cheap enough to re-run often.** Relationship extraction
   needs to happen on every change, not just once. `eng index` walks and
   re-parses the entire repo every time (Step 7 only skips
   *reprocessing* — via content-hash comparison — not re-*reading*).
   That's fine for occasional full indexing; it's wasteful as the
   operation you'd want to run on every save.

Bundling both into one RFC: one makes real graph data exist, the other
makes maintaining it affordable enough to matter.

## Proposal

### Milestone 1 — Relationships (`internal/graph`)

**What can actually be built now, and what can't.** Step 8's own framing
example —

```
Document
    ├── references ADR
    ├── implements Component
    ├── fixes Issue
    ├── belongs to Repository
    └── owned by Team
```

— mixes entities that exist (Document, Repository) with entities that
don't: Issue and a Team/Person aren't ingested or persisted at all yet,
and Component/Service are still DOMAIN_MODEL.md concepts, not real rows
(both explicitly marked Milestone 2+/3+ there, still true). Scoping this
honestly: **Milestone 1 populates Document↔Document edges
(`references`, `supersedes`) and relies on the existing implicit
Document↔Repository edge (the `repository_id` column) — nothing else.**
`implements`, `fixes`, `owned by` stay named in the vocabulary
(KNOWLEDGE_MODEL.md §4) with no producer until their target entities
exist. Building fake stub entities to satisfy the example literally would
be worse than admitting the gap.

**Resolving front-matter references.** This repo's own RFC template uses
bare numbers (`supersedes: 0001`), not paths — the open question
RFC-0001/DATABASE.md left unresolved. Two resolution strategies (see
Alternatives): a naming-convention resolver (glob `NNNN-*.md` in the same
directory a reference appears in) vs. requiring authors write explicit
relative paths. This RFC recommends convention-based resolution for
numeric references, since it's what this repo's real front-matter already
contains, falling back to path resolution when a reference already looks
like a path (contains `/` or ends in `.md`).

**Where extraction lives.** `internal/graph` owns turning a resolved
front-matter reference into a `domain.Relationship` — not `Parser` or
`Normalizer`, so their existing responsibility boundaries (RFC-0002) don't
grow a new "and also resolve cross-document references" clause. `Indexer`
calls into `graph`'s extractor after `Normalize`, before `Chunk` — a
relationship is a property of the document, not of a chunk.

### Milestone 2 — Knowledge Graph

**No new storage engine.** SQLite's `relationships` table plus `WITH
RECURSIVE` queries are sufficient at this kernel's actual scale (a
handful of repos, hundreds of documents) — a dedicated graph database
would be real over-engineering for that corpus size, and would
contradict RFC-0002's "why Indexer stays separate from Storage" reasoning
(one Storage, one place writes rows; a second storage engine reopens
that question for no scale-driven reason).

`internal/graph` exposes traversal on top of `kernel.Storage` — a
`Neighbors` (one hop) and a `Traverse` (bounded depth, default small,
e.g. 2) — not a second backend. Traversal depth is bounded on purpose:
unbounded traversal on a few hundred densely-connected nodes returns
"most of the repo," which isn't a useful answer to anything.

### Milestone 3 — Incremental Indexing (`eng sync`)

`eng index` already skips *reprocessing* unchanged files (Step 7's
content-hash comparison) — but it still *reads and parses every file* to
compute that hash before it can decide "unchanged." `eng sync` is a
different operation: skip reading files at all when they haven't changed,
using git as the source of truth for what changed.

New optional interface, `IncrementalCollector` (`internal/kernel`): a
`Collector` may implement `CollectChanged(ctx, repo, sinceCommit string)
(changed []RawDocument, deleted []string, error)`.
`internal/collector/filesystem` implements it via `git diff --name-status
<since>...HEAD` (plus working-tree changes — see Trade-offs). `Indexer`
gains a `Sync` method that uses `IncrementalCollector` when the
`Collector` implements it, falling back to a full `Index` otherwise (not
a git repo; no prior indexed commit recorded).

**This also finally closes a named gap from Step 7's
[`SPRINT_2_REVIEW.md`](../SPRINT_2_REVIEW.md): deleted files.** `eng sync`
removes a deleted file's `documents`/`document_chunks`/`tags`/
`relationships`/FTS rows. `eng index` deliberately does **not** gain this
— it stays a full walk-and-upsert, unchanged from Step 7; deletion
handling is `eng sync`'s reason to exist, not a fix applied everywhere.

## Alternatives considered

- **A dedicated graph database** (embedded, e.g. an embedded graph engine,
  or external). Rejected for this corpus size — SQLite recursive CTEs
  handle a few hundred nodes/edges trivially, and per RFC-0002's own
  reasoning, a second storage engine is exactly the kind of thing this
  kernel's architecture was built to avoid needing.
- **Resolve `supersedes:`/etc. only via explicit relative paths, never
  bare numbers.** Rejected as the primary strategy: this repo's own RFCs
  already use bare numbers, so path-only resolution couldn't resolve any
  reference that exists in this codebase today.
- **Make `eng sync` the only indexing mode** (retire `eng index`).
  Rejected: a first index of a new repo, or recovery from a deleted/
  corrupted SQLite file, genuinely needs a full walk. Two distinct
  commands (per Step 8's own framing) stay clearer than one command with
  an implicit mode switch.

## Trade-offs & risks

- **Convention-based reference resolution is repo-specific** — it assumes
  `NNNN-*.md` naming. A future Notion-sourced reference (Milestone 4+
  Sources) won't have this convention at all. `graph`'s resolver needs to
  become pluggable per Source/Knowledge Type eventually; not designed
  here, flagged so it isn't forgotten.
- **`eng sync`'s git-diff approach only sees committed changes by
  default.** Uncommitted edits need `git status --porcelain` folded in
  too, or `eng sync` silently misses whatever someone's mid-editing.
  Recommendation in this RFC: include working-tree changes (staged and
  unstaged) alongside the commit-range diff, not just the latter.
- **Bounded traversal depth is a real limit**, not just a safety valve —
  a question genuinely 3+ hops away won't surface. Acceptable for the
  "Review PR #245" scale this Step targets; revisit if real usage shows
  otherwise.

## Rollout

1. `internal/graph` extraction (Milestone 1) before traversal (Milestone
   2) — traversal is pointless without real edges to walk first.
2. `IncrementalCollector` + `eng sync` (Milestone 3) has no dependency on
   `internal/graph` and can land independently or in parallel.
3. Dogfood against this same four-repo workspace (`engineering-kernel`,
   `engineering`, `roadmap`, `vision`) before anything downstream
   (Milestones 4+: embeddings, hybrid search, Context Builder, ranking,
   benchmarks, plugins, public API) gets scoped at all.

## Open questions

- Does numeric-reference resolution need to be scoped per-directory (a
  `supersedes: 0001` inside `rfcs/` shouldn't accidentally match some
  unrelated `0001`-numbered thing elsewhere in the workspace)?
- Should `graph`'s resolver be a `kernel`-level interface (pluggable per
  Source/Knowledge Type) from the start, or is one convention-based
  resolver enough until a second Source exists — mirroring how
  `Normalizer` stayed trivial until there was a second `Parser`?
- Does `eng sync` need its own result type, or does `kernel.IndexResult`
  just grow a `Deleted` count? Leaning toward growing the field: same
  shape, same consumer (`eng status`), one less type to keep in sync.
