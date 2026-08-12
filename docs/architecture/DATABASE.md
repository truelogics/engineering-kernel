---
doc: DATABASE
audience: [human, agent]
status: draft
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Database

> Storage-level shape for [DOMAIN_MODEL.md](DOMAIN_MODEL.md), scoped to what
> [RFC-0001](../../rfcs/0001-engineering-memory-kernel.md) puts in v1. SQLite,
> one file per Workspace. This defines columns and relationships, not SQL
> DDL — that comes with the Storage implementation in Sprint 2.

## Tables

### `repositories`

One row per Repository registered in the Workspace.

| Column | Type | Purpose |
|--------|------|---------|
| `id` | string (pk) | Stable id, generated at `eng init` / first index |
| `name` | string | Short name, e.g. `engineering-kernel` |
| `remote_url` | string | Git remote, for display and re-clone |
| `local_path` | string | Absolute path on disk at index time |
| `last_indexed_commit` | string | SHA of the commit last indexed |
| `last_indexed_at` | timestamp | Drives `eng status` staleness check |

### `documents`

One row per parsed file. This is the table everything else hangs off.

| Column | Type | Purpose |
|--------|------|---------|
| `id` | string (pk) | See DOMAIN_MODEL.md open question: path vs content hash |
| `repository_id` | fk → repositories | Owning repo |
| `path` | string | Repo-relative path |
| `doc_type` | enum | `adr`, `rule`, `standard`, `rfc`, `roadmap`, `readme`, `unknown` |
| `title` | string | From front-matter or first `#` heading |
| `front_matter` | json | Raw parsed front-matter, so unmapped fields aren't lost |
| `body` | text | Full markdown body |
| `content_hash` | string | Detects unchanged files so re-index can skip them |
| `git_author` | string | Last committer, if cheaply available (Architecture's Indexer) |
| `git_updated_at` | timestamp | Last commit touching this file |
| `indexed_at` | timestamp | When this row was last written |

### `document_chunks`

Sub-document units, so Search can return a matched snippet rather than "the
whole file matched." Open question carried from RFC-0001: chunk = whole file
vs per-heading vs fixed window — this table works under any of those answers.

| Column | Type | Purpose |
|--------|------|---------|
| `id` | string (pk) | |
| `document_id` | fk → documents | |
| `chunk_index` | int | Order within the document |
| `heading` | string, nullable | Section heading this chunk falls under, if any |
| `content` | text | The chunk's text — what full-text search actually indexes |

A parallel FTS5 virtual table (e.g. `chunks_fts`) indexes `content` for
Search to query; it's a SQLite index structure, not a distinct data table,
so it isn't listed separately here.

### `tags`

Freeform key/value pairs extracted from a Document's front-matter or inline
metadata — e.g. `severity=error`, `adr_number=0007`, `applies_to=apps/**`.
Generalizes Rule and ADR-specific fields (see [`data-model.md`](data-model.md)'s
draft `Rule`/`Decision` field lists) into one table instead of one column set per
doc type, so Milestone 2's Component/Service tagging reuses this rather than
needing new tables.

| Column | Type | Purpose |
|--------|------|---------|
| `id` | string (pk) | |
| `document_id` | fk → documents | |
| `key` | string | e.g. `severity`, `status`, `adr_number` |
| `value` | string | |

### `relationships`

Explicit or inferred links between Documents — "related files," ADR
supersession, an ADR referencing a PR (once PRs exist in Milestone 2).

| Column | Type | Purpose |
|--------|------|---------|
| `id` | string (pk) | |
| `from_document_id` | fk → documents | |
| `to_document_id` | fk → documents | |
| `relationship_type` | enum | `related`, `supersedes`, `references` |
| `source` | enum | `explicit` (from front-matter/body link) or `inferred` (shared tags) |
| `created_at` | timestamp | |

v1 populates this sparsely (explicit links only — e.g. an ADR's
`supersedes:` field) — in practice, close to never in Step 7, since
nothing yet resolves a front-matter reference to a real `documents.id`.
[RFC-0003](../../rfcs/0003-engineering-intelligence.md)/[GRAPH.md](GRAPH.md)
design that resolution (`internal/graph`, not yet implemented). Inferred
relationships and ranking by relationship strength stay later work (per
ARCHITECTURE.md's "Where later milestones attach").

### `index_state`

One row per Repository, tracking index health for `eng status`.

| Column | Type | Purpose |
|--------|------|---------|
| `repository_id` | fk → repositories (pk) | |
| `document_count` | int | |
| `last_full_index_at` | timestamp | |
| `last_incremental_index_at` | timestamp, nullable | v1 may not support incremental yet — see below |
| `status` | enum | `clean`, `stale`, `indexing`, `error` |

## Relationships between tables

```
repositories 1──* documents
documents    1──* document_chunks
documents    1──* tags
documents    *──* documents        (via relationships)
repositories 1──1 index_state
```

## What v1 actually writes

Matches DOMAIN_MODEL.md's v1 scope: `repositories`, `documents`,
`document_chunks`, and `tags` are populated by `eng index`. `relationships`
is populated only for explicit links parseable from front-matter (e.g. an
ADR's `supersedes:`). `index_state` is maintained on every `eng index` run
so `eng status` has something real to report.

## Open questions

- **Incremental indexing — resolved for "skip reprocessing," open for
  "skip reading."** `eng index` skips *reprocessing* unchanged files via
  `content_hash` comparison (implemented, Step 7) — but it still reads
  and parses every file first to compute that hash. Actually skipping the
  read is `eng sync`'s job, designed in
  [RFC-0003](../../rfcs/0003-engineering-intelligence.md)/[GRAPH.md](GRAPH.md),
  not implemented yet.
- **Document id stability — resolved: path-based.** `domain.NewCanonicalDocument`
  derives `documents.id` from `sha256(repositoryID, path)` (Step 7). A
  renamed file becomes a new row (losing relationship/tag history); an
  edited file keeps its row. This was an open question in this file and
  in DOMAIN_MODEL.md; both are now stale on this point — the decision is
  made and live in code, not still undecided.
- **One SQLite file per Repository vs. one per Workspace.** Affects whether
  `documents.repository_id` is even needed as a foreign key or whether each
  repo gets an entirely separate database file. RFC-0001 leaves this open;
  this schema assumes one Workspace-wide file (simpler joins for
  `relationships` across repos), but that's a default, not a decision.
- **`documents.repository_id` assumes Source = Repository.**
  [`KNOWLEDGE_MODEL.md`](KNOWLEDGE_MODEL.md) generalizes Repository to
  Source (git today, Slack/Jira/Notion later). Renaming the column, or
  leaving it as a v1-specific name until a second Source type actually
  exists, is undecided — flagged there, not resolved here.
- **`relationships.from_document_id`/`to_document_id` assume both ends are
  Documents.** Once Component/Service/Person exist as `entities` rows (see
  KNOWLEDGE_MODEL.md's Core Entities), an edge needs to reference either a
  Document or an Entity. Polymorphic column vs. a union table is open.
- **No table has an `ON DELETE` clause.** Step 7 never deleted a row —
  `eng index` only inserts/upserts. `eng sync`
  ([RFC-0003](../../rfcs/0003-engineering-intelligence.md)/[GRAPH.md](GRAPH.md))
  is the first feature that needs to remove a `documents` row and
  everything hanging off it. Whether that's `ON DELETE CASCADE` in the
  schema or explicit multi-table deletes in `Storage.DeleteDocument` is
  undecided — see GRAPH.md's Open questions.
