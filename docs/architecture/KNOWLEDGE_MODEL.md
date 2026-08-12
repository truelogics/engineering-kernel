---
doc: KNOWLEDGE_MODEL
audience: [human, agent]
status: draft
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Knowledge Model

> Companion to [RFC-0001](../../rfcs/0001-engineering-memory-kernel.md),
> [ARCHITECTURE.md](ARCHITECTURE.md), [DOMAIN_MODEL.md](DOMAIN_MODEL.md), and
> [DATABASE.md](DATABASE.md). Those describe what v1 implements. This
> describes the full shape everything else grows into — read it first if
> you're new here, since it answers the question the rest of the kernel is
> built to serve.

## What is Engineering Knowledge?

**Engineering Knowledge is a graph of Entities connected by Relationships,
evidenced by Documents pulled from Sources.**

Unpacked:

- A **Document** (a markdown file, eventually a PR, a Slack thread, a Jira
  ticket) is *evidence* — a retrievable unit of content, not the knowledge
  itself.
- An **Entity** (a decision, a rule, a service, a person) is a *thing the
  documents are about* — some entities correspond 1:1 with a document (an
  ADR *is* the file that records it); others don't (a "service" has no
  single file, only documents that mention it).
- A **Relationship** connects entities and documents into a graph — "this
  ADR is implemented by this code, which is used by this API, which this
  service depends on."
- Knowledge is what you get when you can walk that graph: not "here's a
  file that mentions authentication" but "here's the decision, what
  implements it, and what depends on it."

Everything below exists to make that graph collectible, storable, and
queryable — first without AI (v1), later with it (Milestone 3+).

## 1. Knowledge Sources

Where knowledge comes from. A Source is anywhere a Document can be
collected from.

**v1 (per [RFC-0001](../../rfcs/0001-engineering-memory-kernel.md)'s
non-goals — filesystem only, no network calls):**

| Source | Notes |
|--------|-------|
| Filesystem | Any file inside a registered Repository |
| Repository | A git repo registered into a Workspace (see Core Entities) |
| Markdown | The only format v1's Parser reads |
| ADR | Markdown with ADR front-matter/convention — a Knowledge Type, not a separate source |
| README | Same — markdown, distinguished by Knowledge Type |
| Configuration | e.g. `engineering/rules/*.yaml` — parsed for Rule entities |

**Future — still local, no external API (Milestone 2):**

- Commit — git log/blame beyond the "last committer" metadata v1 already
  captures opportunistically (see ARCHITECTURE.md's Indexer)
- Release Notes, Wiki (if kept as markdown in-repo)
- API Spec (OpenAPI/protobuf), Database Schema (ironically, a future version
  of this Source could ingest `DATABASE.md` itself)

**Future — external services, real network calls (Milestone 2–4):**

- Issue, Pull Request — via GitHub's API, not git itself
- GitHub (discussions, releases, as distinct from the git data above)
- Slack, Jira, Linear, Notion, Confluence

Each new Source gets its own Parser (see Lifecycle, below) feeding the same
Indexer → Storage → Search pipeline — per ARCHITECTURE.md's "Where later
milestones attach," adding a Source should never require changing Indexer,
Storage, or Search.

## 2. Knowledge Types

What a Document *represents*, independent of where it came from — a
Decision could come from a markdown ADR today or a Notion page later, and
should be classified the same way either time.

```
Architecture · Documentation · Decision · Rule · Code · Issue · Discussion
API · Runbook · Research · Tutorial · Meeting · Roadmap
```

**v1 actually produces:** Architecture, Documentation, Decision, Rule,
Roadmap — everything reachable by parsing markdown in a git repo.
**Future:** Code (needs a code parser, not just markdown), Issue/Discussion
(need those Sources ingested), API (needs a spec-format parser), Runbook
(markdown today, but not yet a distinguished type v1 tags for), Meeting
(needs a source — notes aren't currently written anywhere `eng` reads).

This taxonomy is broader and cleaner than `DATABASE.md`'s current
`documents.doc_type` enum (`adr`, `rule`, `standard`, `rfc`, `roadmap`,
`readme`, `unknown`). **Open question, flagged not resolved:** should
`doc_type` be migrated to this Knowledge Type list directly, or does
`doc_type` stay a narrower implementation enum with Knowledge Type as a
derived/display concept on top? Worth settling before Milestone 2 adds
enough types that the two lists visibly diverge.

## 3. Core Entities

The permanent objects in the kernel — the vocabulary everything else is
built from.

| Entity | What it is |
|--------|-----------|
| **Workspace** | The scope of one index — one or more Repositories indexed together |
| **Repository** | A git repo registered into a Workspace. v1's only Source type |
| **Source** | The general concept Repository is a v1 instance of — where a Document was collected from. v1 hard-codes Source = Repository; Milestone 2+ adds Source types that aren't git repos |
| **Document** | A retrievable unit of content from a Source — v1: a markdown file. Not tied to "file on disk": once a GitHub Source exists, a PR is a Document too |
| **Chunk** | A sub-Document unit Search actually indexes (see `document_chunks` in DATABASE.md) |
| **Entity** | A named thing Documents are *about*, which may or may not correspond 1:1 with a Document. An ADR *is* a Document (1:1). A Service is not — it's referenced by many Documents but has no file of its own |
| **Relationship** | A directed, typed edge between two Documents, two Entities, or a Document and an Entity |
| **Metadata** | Structured, schema'd data attached to a Document or Entity — front-matter fields, an ADR's `status`, a Rule's `severity` |
| **Tag** | Freeform key/value data, for anything Metadata's schema doesn't cover yet (see DATABASE.md's `tags` table) |
| **Knowledge** | Not a stored row — the graph itself, once Entities and Relationships exist to walk. This is the answer to "what is Engineering Knowledge," made queryable |

### How this reconciles with DOMAIN_MODEL.md

[DOMAIN_MODEL.md](DOMAIN_MODEL.md) lists ADR, Rule, Decision, Pull Request,
Issue, Component, Service, and Person as entities in their own right. That's
not a competing model — it's what the two general concepts above look like
once named concretely for v1's scope:

| DOMAIN_MODEL.md's entity | Which general concept it is |
|---|---|
| ADR, Rule | A **Document**, classified by Knowledge Type — corresponds 1:1 with a file |
| Decision | Currently derived from an ADR Document; not yet an independent **Entity** row (see DOMAIN_MODEL.md's own open question on this) |
| Component, Service | An **Entity** with no Document of its own — exactly the case Entity exists for |
| Pull Request, Issue | A future **Document**, once a GitHub Source is ingested (Milestone 2) |
| Person | A future **Entity** with no Document of its own (Milestone 3+) |

Nothing here changes DOMAIN_MODEL.md's v1 scope (still: Workspace,
Repository, Document, ADR, Rule parsed-only, Decision derived). It explains
*why* those are the right v1 slice: they're the Documents and Knowledge
Types reachable without a database-shaped `entities` table, which v1
deliberately doesn't build yet (DATABASE.md has no `entities` table —
Component/Service/Person stay undesigned at the storage level until
Milestone 2 actually needs one).

## 4. Relationships

The graph, illustrated:

```
Document ──references──▶ ADR ──implemented_by──▶ Code
                                                    │
                                              used_by
                                                    ▼
                                                   API ──depends_on──▶ Service
```

Relationship types are an open, growing vocabulary — not a fixed enum,
because new Sources and Knowledge Types keep making new edges meaningful.
v1 and near-term types:

| Type | Meaning | Ships in |
|------|---------|----------|
| `related` | Generic association (shared tags, nearby topic) | v1 |
| `references` | Explicit link from one Document to another | v1 |
| `supersedes` | An ADR replacing an earlier one | v1 |
| `implements` / `implemented_by` | A decision realized in code | Milestone 2+ (needs a Code source) |
| `used_by` / `depends_on` | Structural dependency between API/Service Entities | Milestone 2 (needs Component/Service as real Entities, not just DOMAIN_MODEL.md concepts) |
| `owns` | Person ↔ Component/Service | Milestone 3+ (needs Person) |

**Direction is stored once, not twice.** `implements` and `implemented_by`
are the same edge read from either end — DATABASE.md's `relationships` table
stores one directed row (`from → to, type`); reading the inverse direction
is a query-time concern for Search/Retriever, not a second row to keep in
sync. **Open question carried into DATABASE.md:** that table's
`from_document_id`/`to_document_id` columns assume both ends are Documents.
Once Entities (Component, Service, Person) exist as their own rows, those
columns need to reference either a Document or an Entity — a polymorphic
reference, or a union table. Not designed yet; flagged here so it isn't
forgotten when Milestone 2 needs it.

## 5. Lifecycle

How knowledge flows, end to end:

```
Source → Collect → Parse → Normalize → Chunk → Index → Store → Search → Retrieve → Context
```

[ARCHITECTURE.md](ARCHITECTURE.md)'s Pipeline diagram is this same shape,
concretely: `Source` above is named `Filesystem` there (what it actually is
in v1), and each verb (`Collect`, `Parse`, …) is named for the interface
that does it (`Collector`, `Parser`, …, per [INTERFACES.md](INTERFACES.md)).
This is the general, source-agnostic version; ARCHITECTURE.md is the
concrete v1 instance of it. Only `Normalize` is a no-op in v1 — markdown is
already one canonical format, so there's nothing to reconcile until a
second, non-markdown Source exists.

## Open questions

- Should `documents.doc_type` (DATABASE.md) be migrated to this document's
  Knowledge Type list, or kept as a narrower implementation detail?
- When do Component, Service, and Person graduate from DOMAIN_MODEL.md
  concepts to actual `entities` rows in storage — is that the trigger for
  Milestone 2, or does Milestone 2 start elsewhere and entities come later?
  **Partially answered by [RFC-0003](../../rfcs/0003-engineering-intelligence.md):**
  Step 8's relationship work (Milestone 1-2) still doesn't add an
  `entities` table — it only populates Document↔Document edges. This
  question stays open for Component/Service/Person specifically.
- Does `relationships` need a polymorphic Document-or-Entity reference now,
  or is it safe to defer until the first non-Document Entity actually
  ships? **Still deferred** — RFC-0003's graph work is Document↔Document
  only, so `relationships.from_document_id`/`to_document_id` don't need
  to change yet.
- Is "Normalize" ever a real, separate component, or does it always end up
  absorbed into each Source's Parser (i.e., a Slack parser just emits an
  already-normalized Document, the same way the markdown Parser does today)?
