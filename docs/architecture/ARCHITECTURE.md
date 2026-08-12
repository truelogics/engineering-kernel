---
doc: ARCHITECTURE
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Architecture

> Companion to [RFC-0001](../../rfcs/0001-engineering-memory-kernel.md),
> [RFC-0002](../../rfcs/0002-knowledge-engine.md) (why this pipeline shape),
> [KNOWLEDGE_MODEL.md](KNOWLEDGE_MODEL.md), and
> [INTERFACES.md](INTERFACES.md). This describes the v1 kernel only — no AI,
> no embeddings, no agents.
>
> **Status: implemented.** Every component below has a concrete package
> under `internal/` (linked in its section) satisfying the interface
> named here — `eng init`, `eng index`, `eng search`, and `eng status`
> all work end-to-end. See [`../../CHANGELOG.md`](../../CHANGELOG.md) and
> [`../../SPRINT_2_REVIEW.md`](../../SPRINT_2_REVIEW.md) for what shipped
> and what's still a known gap.

## Pipeline

This is the one diagram everything else in this repo's docs points back
to — [KNOWLEDGE_MODEL.md](KNOWLEDGE_MODEL.md)'s Lifecycle and
[INTERFACES.md](INTERFACES.md)'s ten interfaces are both this same shape,
described at a different level (Lifecycle: the general, source-agnostic
version; Interfaces: the Go seam per stage). This is the concrete v1
instance of both — same stages, named for what they actually are today.

```
Filesystem
      │
      ▼
  Collector       reads a file's bytes off disk
      │
      ▼
   Parser         turns bytes + path into a structured Document
      │
      ▼
  Normalizer      reconciles Document into one canonical shape (pass-through in v1)
      │
      ▼
  Chunker         splits a Document into Chunks — the unit Search indexes
      │
      ▼
  Indexer         orchestrates the above per file, writes via Storage
      │
      ▼
  Storage         persists records (SQLite)
      │
      ▼
   Search         ranked full-text query over Storage
      │
      ▼
  Retriever       gathers everything relevant for a task into a bundle
      │
      ▼
Context Builder   packages the bundle for a consumer — v1: CLI text. Later: an LLM prompt
```

Every arrow is a local, synchronous function call. Nothing in this diagram
makes a network call, calls a model, or leaves the machine. `Filesystem` is
the pipeline's one open end, not a component with its own interface —
it's what v1's `Source` reads from. `Context Builder` is a real component
(see INTERFACES.md); what it produces (`Context`) is the pipeline's output,
consumed by `cmd/eng` today.

## Components

Full responsibilities for every component below live in
[INTERFACES.md](INTERFACES.md). This section is the shorter, narrative
version for the five that are more than a pass-through in v1.

### Collector (`internal/collector/filesystem`)

Reads a file's raw bytes off disk, given a path `Indexer` found by walking a
`Repository`. Thin in v1 — a wrapped `os.ReadFile` — but its own seam so a
future non-filesystem `Collector` (an HTTP call) doesn't require touching
`Parser`.

### Parser (`internal/parser/markdown`)

Takes the bytes `Collector` fetched and produces a `Document` (see
[DOMAIN_MODEL.md](DOMAIN_MODEL.md)): path, front-matter, body, doc type,
content hash. v1 parses markdown only, using goldmark's CommonMark+GFM AST
(not a regex) so headings, code blocks, links, and tables parse correctly.
Doc type (`adr`, `rule`, `standard`, `roadmap`, `readme`, …) is inferred
from front-matter `doc:` field and path conventions, falling back to
`unknown`.

Responsibility boundary: parsing has no opinion about storage or ranking. It
turns bytes into a typed struct and nothing else — it doesn't know SQLite
exists.

### Normalizer (`internal/normalizer`)

Reconciles a `Parser`'s output into one canonical `Document` shape. v1 is a
pass-through — markdown's `Parser` output already is that shape — kept as
its own step so a second `Parser` (Milestone 2+) has somewhere to reconcile
into, instead of `Chunker` needing to special-case every source format.

### Chunker (`internal/chunker`)

Splits a normalized `Document` into `document_chunks` (see
[DATABASE.md](DATABASE.md)) — the unit `Search` actually indexes, so a query
returns a matched section instead of "the whole file matched."

### Indexer (`internal/indexer`)

Walks a `Repository`, calling `Collector` → `Parser` → `Normalizer` →
`Chunker` per file, then writes the resulting records via `Storage`: the
document row, its chunks, and any tags extracted from front-matter. Also
records `git` metadata available cheaply (last commit, author) without doing
full history analysis in v1. Owns incremental-index decisions (skipping
unchanged files via `content_hash`).

Responsibility boundary: indexing decides *what* gets stored, not *how* it's
queried later. It writes once per `eng index` run; it never reads back its
own output to answer a query.

### Storage (`internal/storage/sqlite`)

SQLite adapter (via `modernc.org/sqlite`, pure Go — no cgo). Owns the schema
([`DATABASE.md`](DATABASE.md)) and all reads/writes, including the FTS5
index `Search` queries. Every other component talks to Storage through a
narrow interface (`PutDocument`, `PutChunks`, `SearchChunks`, …) — no
component reaches into SQLite directly except this one, so swapping the
backing store later doesn't ripple outward. `PutDocument` and `PutChunks`
each write their rows (document + tags + relationships; chunks + FTS
index) inside one transaction, so a failure partway through can't leave
half-written state.

### Search (`internal/search`)

Takes a query string, fetches a wider candidate pool than requested from
Storage's full-text index (SQLite FTS5), then blends signals before
truncating to the requested count (Step 8, Milestone 5 — "hybrid
search"): keyword (BM25, normalized), graph (a candidate connected to
other candidates in the *same* result set is boosted — sparse today,
since explicit Relationships are still rare), and, only if an
`EmbeddingProvider` is configured (none is, in any real deployment yet —
Milestone 4 shipped no provider), semantic similarity. Blend weights
default to Milestone 5's values but are configurable via `Search.Weights`
(Milestone 7), and `Retriever`'s section order is configurable the same
way via `Retriever.Priority`. Returns a ranked list: file, blended score,
matched snippet, related files. "Related" tries explicit Relationships
first, then falls back to documents sharing a non-structural Tag. This is
what `eng search` calls
directly.

### Retriever (`internal/retriever`)

Gathers everything relevant for a task — not just a search query.
Implemented in Step 8's Milestone 6: turns a free-text task ("Review
authentication PR") into an FTS query (stopwords dropped, remaining terms
OR'd — a bareword multi-term match is an implicit AND, too strict for a
natural sentence), searches via the hybrid `Search`, and groups results by
Knowledge Type into a labeled bundle (Architecture, Related ADRs, Rules,
Related RFCs, Roadmap, Documentation). "Related Issues"/"Related PRs" are
always present but empty — RFC-0001's non-goals mean nothing ingests those
yet, and showing an empty section says so explicitly instead of silently
omitting it. No generation, no synthesis of a prose answer — the value is
better *assembly* of what Search already found, not new intelligence.

### Context Builder (`internal/contextbuilder`)

Packages a `Retriever` bundle into whatever a consumer needs. Implemented
in Milestone 6, and deliberately thin: v1's only consumer is a terminal, so
`Build` formats the bundle as readable text (`eng context`/`eng ask`'s
output) — including printing "(none indexed yet)" for empty sections
rather than dropping them. This is the seam where a future AI layer plugs
in later — it would call `Context Builder` for a packaged prompt instead
of formatting `Retriever`'s bundle itself, so `Retriever` never has to
know or care what its output gets turned into.

### CLI (`cmd/eng`, `internal/cli`)

Thin layer translating commands into calls against the components above,
plus formatting output for a terminal — the actual translation logic lives
in `internal/cli` so it's testable without spawning the binary; `cmd/eng`
just parses `os.Args` and calls in. Of the eight commands in
[`CLI.md`](../cli/CLI.md) plus `eng context` (Milestone 6, not in the
original seven), six are implemented (`init`, `index`, `sync`, `search`,
`status`, `ask`/`context`); `add`, `doctor` print "not yet implemented" and
exit non-zero. No business logic lives in `cmd/eng` itself — if it were
deleted and replaced with an HTTP handler tomorrow, none of the components
above would change.

## Non-goals reflected in this design

- No component here calls out to a model or an embedding API — Search,
  Retriever, and Context Builder are pure functions over Storage's results.
- No component assumes a single repo — `Workspace` (see DOMAIN_MODEL.md) can
  span `engineering-kernel`, `engineering`, `roadmap`, `vision` from day one, even
  though v1 may only be exercised against one repo at a time.
- No component is network-facing. `eng` is a local CLI against a local
  SQLite file.

## Where later milestones attach

This pipeline is deliberately built so Milestones 2–4 are additive, not
rewrites:

- **Milestone 2 (Intelligence):** a `relationships` table and ranking model
  sit between Storage and Search — Search's interface doesn't change, its
  results just get better.
- **Milestone 3 (AI layer):** a new generation component calls Context
  Builder for a packaged prompt and an LLM for generation, sitting *after*
  Context Builder in the pipeline — Retriever and Context Builder don't
  change.
- **Milestone 4 (Engineering OS):** PR/planning/bug intelligence are new
  Parsers (PR, Issue) feeding the same Indexer → Storage → Search path.

If a future milestone requires changing Parser, Indexer, or Storage's core
contract to fit, that's a signal the v1 design needs revisiting — not a
reason to bolt something on sideways.
