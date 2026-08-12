---
doc: INTERFACES
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Interfaces

> Companion to [KNOWLEDGE_MODEL.md](KNOWLEDGE_MODEL.md) (Lifecycle),
> [ARCHITECTURE.md](ARCHITECTURE.md) (pipeline), and
> [RFC-0002](../../rfcs/0002-knowledge-engine.md) (why each stage below
> exists rather than a simpler shape). Names the Go interface at each
> pipeline stage — `Source → Collector → Parser → Normalizer → Chunker →
> Indexer → Storage → Search → Retriever → Context Builder` — so the seam
> between stages exists before any implementation does.
>
> **Status: eight of ten implemented.** This was originally design-only
> (no methods); Step 7 implemented real method signatures in
> `internal/kernel` and concrete types for everything except `Retriever`
> and `Context Builder`, which stayed interface-only since `eng ask` wasn't
> in Step 7's scope. See the table below for exact package locations.
>
> **Step 8 adds one more, `IncrementalCollector`** — an optional interface
> `Collector` implementations may satisfy, designed (not implemented) in
> [GRAPH.md](GRAPH.md) alongside `internal/graph`'s relationship
> extraction and traversal. See [RFC-0003](../../rfcs/0003-engineering-intelligence.md).

## Why interfaces before implementation

Sprint 2 will write real code against these names. Deciding the boundary now
— what each interface owns, what it deliberately doesn't, what flows in and
out — means the first concrete implementation slots into a shape already
agreed on, instead of the shape getting discovered (and re-argued)
mid-implementation. It also means v1 can ship a trivial implementation
behind an interface (e.g. `Normalizer` doing nothing to a markdown Document)
without that being a hack — Milestone 2 swaps the implementation, not the
callers.

## The canonical document model

Before the interfaces: the one shape they all either produce or consume.
This is the contract every `Parser` must eventually satisfy (via
`Normalizer`, if not directly) and every `Storage` must be able to persist.

```
RawDocument
- Path          where Collector found it (repo-relative or a source-specific id)
- SourceID      which Source/Repository it came from
- Bytes         the raw, unparsed content
- FetchedAt     when Collector fetched it

CanonicalDocument
- ID            stable identity (see DATABASE.md's open question: path vs. content hash)
- Source        which Source/Repository this came from (KNOWLEDGE_MODEL.md: Source)
- Type          a Knowledge Type (KNOWLEDGE_MODEL.md §2) — adr, rule, standard, roadmap, readme, …
- Title         from front-matter or first heading
- Content       the body, post-parsing
- Metadata      structured, schema'd fields (front-matter) — KNOWLEDGE_MODEL.md: Metadata
- Tags          freeform key/value pairs — KNOWLEDGE_MODEL.md: Tag
- Relationships edges to other Documents/Entities — KNOWLEDGE_MODEL.md: Relationship
```

`RawDocument` is `Collector`'s output and `Parser`'s input — bytes with just
enough provenance to trace them back to a `Source`. `CanonicalDocument` is
what `Normalizer` guarantees regardless of which `Parser` produced the input
— `Chunker`, `Indexer`, `Storage`, `Search`, and `Retriever` all depend on
this shape and nothing more specific. Field-by-field mapping to storage:
`Metadata` → `documents.front_matter`, `Tags` → the `tags` table,
`Relationships` → the `relationships` table (see DATABASE.md).

## Pipeline stages at a glance

| Stage | Receives | Produces | Must never |
|---|---|---|---|
| `Source` | A Workspace registration (repo path, remote) | An enumeration of collectible items (e.g. file paths) — not content | Fetch content itself |
| `Collector` | An item a `Source` enumerated | `RawDocument[]` | Interpret content, or decide what's worth collecting |
| `Parser` | A `RawDocument` | A structured document (ideally already `CanonicalDocument`-shaped) | Chunk, persist, or assume a `Normalizer` will fix its mistakes |
| `Normalizer` | A `Parser`'s output | A guaranteed `CanonicalDocument` | Fetch content or know about specific `Source`/`Parser` types |
| `Chunker` | A `CanonicalDocument` | `Chunk[]` | Persist chunks or rank them |
| `Indexer` | A `Source` to walk | Orchestrated writes via `Storage` (no return value to a caller — it's the side effect) | Execute SQL directly, or decide ranking |
| `Storage` | `CanonicalDocument` / `Chunk[]` / `Tag[]` / `Relationship[]` to write, or a query to read | Persisted rows, or query results | Decide what's stale (`Indexer`'s job) or rank relevance (`Search`'s job) |
| `Search` | A query string (+ optional filters) | Ranked `SearchResult[]` | Write to `Storage`, or assemble a multi-result bundle for a task |
| `Retriever` | A task (a question, or something broader — "Review PR #123") | A `RetrievalBundle` — labeled, grouped results | Format for a specific consumer, or generate prose |
| `Context Builder` | A `RetrievalBundle` | `Context` — packaged for whatever consumes it | Decide what's relevant (that's `Retriever`'s job), or call a model itself |

Full detail per stage follows.

## `Source`

```go
type Source interface{}
```

**Package:** `internal/source` (new)

**Responsibility:** Represents where Documents originate — v1: a registered
Repository (a git repo path on local disk). Owns identity (which Workspace/
Repository this is) and enumeration (what's collectible from it — e.g. which
file paths under this repo are worth looking at).

**Receives:** A Workspace registration — repo path and remote URL (what
`eng add` captures).

**Produces:** An enumeration of collectible items. In v1, a list of file
paths under the repo matching known patterns (markdown). Not content itself.

**Must never:** Fetch bytes (`Collector`'s job) or interpret content
(`Parser`'s job). A `Source` can be enumerated without fetching anything.

**v1:** One concrete type — a local git repository. Real, not trivial:
`eng add`/`eng index` genuinely need repo enumeration in v1, unlike
`Normalizer` below.

Examples beyond v1 (not implemented, named for scope): Filesystem, GitHub,
GitLab, Notion, Slack, Local directory — each is a `Source` implementation,
not a change to the `Source` interface itself.

## `Collector`

```go
type Collector interface{}
```

**Package:** `internal/collector` (new)

**Responsibility:** Given something a `Source` enumerated, fetches its raw
bytes. v1: `os.ReadFile` against a local path. Future: an HTTP call against
GitHub's/Slack's/Notion's API. This is the only place actual I/O for
fetching content happens.

**Receives:** `Source` (an item reference from its enumeration).

**Produces:** `RawDocument[]` — raw bytes plus minimal provenance (path,
source id, fetch time). Not parsed, not structured.

**Must never:** Decide *what* to fetch (`Source`'s job) or *interpret* what
was fetched (`Parser`'s job).

**v1:** Real, but thin — a local filesystem read. Still its own interface
so a future non-filesystem `Collector` doesn't require touching `Source` or
`Parser`.

## `Parser`

```go
type Parser interface{}
```

**Package:** `internal/parser` (exists — see ARCHITECTURE.md)

**Responsibility:** Converts raw content into a structured document:
front-matter, body, and an inferred doc type. v1: markdown only.

**Receives:** A `RawDocument`.

**Produces:** A structured document — ideally already `CanonicalDocument`-
shaped, though not guaranteed until `Normalizer` runs.

**Must never:** Chunk, persist, or assume cross-`Source` consistency — a
`Parser` doesn't know a `Normalizer` exists downstream, and doesn't know
SQLite exists at all.

**v1:** Real — this is the first component Sprint 2 actually implements.

## `Normalizer`

```go
type Normalizer interface{}
```

**Package:** `internal/normalizer` (new)

**Responsibility:** Transforms whatever a `Parser` produced into one
canonical `CanonicalDocument` shape, regardless of source-format quirks —
so a future Slack-thread `Parser`'s output and today's markdown `Parser`'s
output both come out the other side identical in shape for `Chunker` to
consume.

**Receives:** A `Parser`'s output (format-specific, possibly not fully
canonical).

**Produces:** A guaranteed `CanonicalDocument`.

**Must never:** Fetch content, or know about specific `Source`/`Parser`
types — it operates on whatever structured document it's handed, generically.

**v1:** Trivial — markdown's `Parser` output already *is* the canonical
shape, so v1's `Normalizer` is a pass-through. The interface exists so the
seam is there when a second, non-markdown `Parser` needs it — not because
v1 has real normalization work to do.

## `Chunker`

```go
type Chunker interface{}
```

**Package:** `internal/chunker` (new)

**Responsibility:** Splits a large `CanonicalDocument` into searchable
Chunks — the unit `Search` actually indexes (see DATABASE.md's
`document_chunks`). Owns the chunking strategy: whole-file, per-heading, or
fixed-window (an open question in RFC-0001, still unresolved here — this
interface is where that decision gets implemented once made).

**Receives:** A `CanonicalDocument`.

**Produces:** `Chunk[]` — ordered sub-document units referencing their
parent `CanonicalDocument`'s ID.

**Must never:** Persist chunks (`Indexer`/`Storage`'s job) or rank them
(`Search`'s job).

**v1:** Real — chunking has to exist for `eng search` to return a snippet
instead of "the whole file matched."

## `Indexer`

```go
type Indexer interface{}
```

**Package:** `internal/indexer` (exists — see ARCHITECTURE.md)

**Responsibility:** Orchestrates one `eng index` run — walks a `Source`,
calls `Collector` → `Parser` → `Normalizer` → `Chunker` per item, and hands
the results to `Storage`. Owns incremental-index decisions (skipping
unchanged files via `content_hash`, per DATABASE.md).

**Receives:** A `Source` to walk.

**Produces:** No return value to a caller — its output is the side effect
of writes made through `Storage` (document rows, chunk rows, tag rows,
`index_state` updates).

**Must never:** Execute persistence mechanics directly (`Storage`'s job) or
interpret file formats itself (`Parser`'s job).

**v1:** Real.

## `Storage`

```go
type Storage interface{}
```

**Package:** `internal/storage` (exists — see ARCHITECTURE.md)

**Responsibility:** Persists documents, chunks, metadata, tags, and
relationships. The only component that touches SQLite. Owns reads and
writes for every table in DATABASE.md (`repositories`, `documents`,
`document_chunks`, `tags`, `relationships`, `index_state`).

**Receives:** `CanonicalDocument` / `Chunk[]` / `Tag[]` / `Relationship[]`
to write; a structured query to read.

**Produces:** Persisted rows (on write) or query results (on read).

**Must never:** Decide what's stale or worth reindexing (`Indexer`'s job)
or rank relevance (`Search`'s job).

**v1:** Real — SQLite.

## `Search`

```go
type Search interface{}
```

**Package:** `internal/search` (exists — see ARCHITECTURE.md)

**Responsibility:** Retrieves matching knowledge — ranked full-text query
over `Storage`.

**Receives:** A query string, plus optional filters (repo, doc type, limit).

**Produces:** Ranked `SearchResult[]` — file/chunk reference, score, matched
snippet, related files.

**Must never:** Write to `Storage`, or assemble multiple results into a
labeled bundle for a task (`Retriever`'s job).

**v1:** Real — SQLite FTS5.

## `Retriever`

```go
type Retriever interface{}
```

**Package:** `internal/retriever` (exists — see ARCHITECTURE.md)

**Responsibility:** Gathers everything relevant for a task — not just a
search query. `eng ask`'s natural-language question is one task shape;
"Review PR #123" is another. For the latter, `Retriever` should return
related documents, related ADRs, similar PRs, architecture, and rules —
grouped and labeled, not flattened into one list.

**Receives:** A task — v1: a question (`eng ask`). Extracts search terms,
calls `Search` with them.

**Produces:** A `RetrievalBundle` — labeled, de-duplicated groups of
`SearchResult`s (e.g. "Architecture docs", "ADRs", "Related PRs" — PRs empty
until Milestone 2 ingests them).

**Must never:** Format output for a specific consumer (CLI text vs. an LLM
prompt — that's `Context Builder`'s job) or generate prose. Assembly only,
no generation.

**v1:** Real, heuristic — see CLI.md's `eng ask` for the current shape of
"real." The PR-review task shape above is illustrative of where `Retriever`
is headed, not implemented in v1 — v1 has no PR ingestion (RFC-0001
non-goals), so `Retriever` can't yet return "similar PRs" for anything.

## `Context Builder`

```go
type ContextBuilder interface{}
```

**Package:** `internal/contextbuilder` (new)

**Responsibility:** Packages a `RetrievalBundle` into a format a consumer
can use. v1's only consumer is a terminal — `Context Builder` formats the
bundle as readable CLI output. Milestone 3's consumer is an LLM — the same
`RetrievalBundle` gets packaged as a token-budget-aware prompt instead.
`Retriever` doesn't change either way; only `Context Builder`'s output shape
does.

**Receives:** A `RetrievalBundle` from `Retriever`.

**Produces:** `Context` — the packaged form. v1: formatted text for
`cmd/eng`'s stdout. Future: a structured prompt.

**Must never:** Decide what's relevant (`Retriever`'s job) or call a model
itself — generation is a separate, later component that consumes
`Context Builder`'s output, not part of it.

**v1:** Real but minimal — v1's `Context` is CLI-formatted text, which is
close enough to `Retriever`'s `RetrievalBundle` that this stage is closer to
`Normalizer` in triviality than to `Parser` in complexity. Still named
because Milestone 3 needs the seam without touching `Retriever`.

## What v1 actually builds, and where

| Interface | v1 implementation | Package |
|---|---|---|
| Source | Real — local git repository | `domain.Repository` (a domain model, not its own package — see INTERFACES.md's own open question below, since resolved: v1 never gave `Source` an independent interface) |
| Collector | Real, thin — local filesystem read | [`internal/collector/filesystem`](../../internal/collector/filesystem/) |
| Parser | Real — markdown, goldmark-based | [`internal/parser/markdown`](../../internal/parser/markdown/) |
| Normalizer | Real, near-trivial — defaults, dedup, path cleaning | [`internal/normalizer`](../../internal/normalizer/) |
| Chunker | Real — heading/paragraph/fixed-size strategies | [`internal/chunker`](../../internal/chunker/) |
| Indexer | Real | [`internal/indexer`](../../internal/indexer/) |
| Storage | Real — SQLite (`modernc.org/sqlite`) | [`internal/storage/sqlite`](../../internal/storage/sqlite/) |
| Search | Real — FTS5 + related-document lookup | [`internal/search`](../../internal/search/) |
| Retriever | **Not implemented** — interface only | `internal/kernel` |
| Context Builder | **Not implemented** — interface only | `internal/kernel` |

Two deviations from what this document originally predicted, worth
recording rather than silently editing away:

- **`Normalizer` turned out not to be fully trivial.** It really does
  validate identity, deduplicate Tags/Relationships, and clean paths (see
  its own tests) — "trivial" undersold it. Still no format-reconciliation
  logic, since there's still only one Parser.
- **`Retriever` and `Context Builder` are not implemented**, despite this
  document originally predicting "Real, heuristic" and "Real, minimal."
  Step 7's actual scope (`eng init/index/search/status`) never needed
  them — `eng ask` didn't make the cut. The interfaces exist in
  `internal/kernel` so Milestone 3+ has the seam ready.

## Extension points

The acceptance test for this design is whether each of these is additive —
a new implementation of an existing interface — rather than a change to the
kernel:

- **New collector** (e.g. GitHub): implement `Collector` (and a matching
  `Source`) for the GitHub API. `Parser`, `Normalizer`, `Chunker`,
  `Indexer`, `Storage`, `Search`, `Retriever` don't change.
- **New parser** (e.g. Notion): implement `Parser` for Notion's content
  shape, producing whatever `Normalizer` can reconcile into
  `CanonicalDocument`. `Collector` and everything after `Normalizer` don't
  change.
- **New embedding provider** (OpenAI, Ollama, Voyage, Gemini, FastEmbed, …):
  implement `kernel.EmbeddingProvider` (`Embed`, `Dimensions`) — named and
  designed in Step 8's Milestone 4, deliberately with zero implementations
  shipped, so the shape came from what embeddings are used *for* (Milestone
  5's hybrid search) rather than being copied from whichever provider's SDK
  got implemented first. `Storage`'s schema has no vector column yet —
  that's Milestone 5's problem once a real provider exists to size it
  against.
- **New storage engine** (SQLite → Postgres): implement `Storage` against
  Postgres. `Indexer`, `Search`, `Retriever`, and the CLI are unaffected —
  none of them touch SQL directly, only the `Storage` interface.
- **New search engine** (e.g. a vector database): implement `Search`
  against it, or compose it alongside the existing `Search` implementation
  behind one `Search` interface. Which of those two shapes is right is a
  Milestone 2 design question, not resolved here.
- **New retriever** (task-specific gathering — a PR-review-shaped
  `Retriever` vs. a question-shaped one): implement `Retriever` for the new
  task type. `Context Builder` consumes whatever `RetrievalBundle` it gets
  uniformly, regardless of which `Retriever` produced it.

## Acceptance criteria

Another engineer should be able to answer each of these from this document
alone, without changing the core architecture:

- **How do I add a GitHub collector?** Implement `Collector` (and a
  `Source`) for GitHub. Nothing downstream of `Normalizer` changes.
- **How do I add a Notion parser?** Implement `Parser` for Notion's shape.
  `Normalizer` already exists to reconcile it with markdown's output.
- **How do I replace SQLite with PostgreSQL?** Implement `Storage` against
  Postgres. Every other component only ever calls the `Storage` interface.
- **How do I plug in a vector database?** Implement (or extend) `Search`.
  The exact shape — replace vs. compose — is an open Milestone 2 question,
  but either way it's a `Search`-level change, not a kernel-wide one.
- **How do I create a new retriever?** Implement `Retriever` for the new
  task shape. `Context Builder` and everything upstream of `Retriever`
  don't change.

## Open questions

- Do `Collector` and `Source` end up merged into one interface in practice,
  since v1's `Collector` is trivial enough (`os.ReadFile`) that splitting it
  from `Source`'s enumeration might be over-abstraction for a single-Source
  system? Left as two per KNOWLEDGE_MODEL.md's Lifecycle, to be revisited
  once method signatures are actually drafted.
- Where do cross-cutting concerns — error handling, logging, context
  cancellation — attach? Not decided; deferred until methods are designed.
- Does `Chunker`'s strategy need to be pluggable per doc type (an ADR chunks
  differently than a README), or is one strategy enough for v1?
- Does `Context Builder` stay its own component once its v1 implementation
  is "format as text," or does it get folded into `cmd/eng` until Milestone
  3 actually needs a second output shape?
- What does the vector-database extension point (`Search`, above) actually
  look like — a second `Search` implementation, or a ranking layer that
  composes both full-text and vector results? Not designed; flagged so
  Milestone 2 doesn't have to rediscover that this is a real decision.
