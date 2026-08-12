---
doc: data-model
audience: [human, agent]
status: draft
owner: engineering-kernel
last_reviewed: 2026-07-30
---

# Data model

Core entities Engineering Kernel stores and how they relate.

> **Status:** superseded by [`DATABASE.md`](DATABASE.md), the finalized v1
> schema per [RFC-0001](../../rfcs/0001-engineering-memory-kernel.md). Kept
> here for history; new schema work happens in `DATABASE.md`.

## Entities (draft)

### Document

A source artifact: markdown file with front-matter from `engineering/`, `roadmap/`, etc.

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Stable id (path hash or explicit `doc` key) |
| `path` | string | Repo-relative path |
| `doc_type` | enum | `rule`, `adr`, `rfc`, `standard`, `roadmap`, … |
| `front_matter` | object | Parsed YAML |
| `body` | string | Markdown prose |
| `content_hash` | string | For change detection |

### Rule

Machine-enforceable directive, often from `engineering/rules/*.yaml` or inline
`rules` blocks.

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Stable kebab-case id |
| `severity` | enum | `error`, `warn`, `info` |
| `applies_to` | string[] | Glob patterns |
| `rule` | string | Checkable imperative sentence |
| `source_document_id` | string | Link to parent Document |

### Decision (ADR)

Architectural decision record — immutable once accepted.

| Field | Type | Notes |
|-------|------|-------|
| `adr_number` | string | e.g. `0007` |
| `title` | string | |
| `status` | enum | `proposed`, `accepted`, `deprecated`, … |
| `context` | string | |
| `decision` | string | |
| `consequences` | string | |

## Relationships (draft)

```
Document 1──* Rule        (rules extracted from or linked to a doc)
Document 1──1 Decision    (ADR docs)
Document *──* Document    (cross-links by path or id)
```

## Open questions

- Storage: files-only index vs embedded DB vs vector store for MVP?
- Identity: path-based vs content-addressed ids across repos?
- Versioning: snapshot per ingest or track history?

_See RFC-0001 for resolution._
