---
rfc: 0001
title: Engineering Memory Kernel
status: draft
author: founding-team
created: 2026-08-01
supersedes:
superseded_by:
resulting_adr:
---

# RFC 0001 — Engineering Memory Kernel

## Summary

Build the kernel of the AI Engineering OS: a CLI-driven system that stores,
indexes, and searches engineering knowledge (markdown docs, ADRs, git
metadata) using plain full-text search — no LLM, no embeddings, no agents.
If `eng index && eng search "authentication"` reliably returns the right ADR,
README, and related files, the kernel has earned the right to grow a context
and intelligence layer on top of it. If it can't do that, no amount of AI
on top will fix it.

## Motivation

Agents start every session with no memory. Humans lose context to scattered
markdown, stale wikis, and tribal knowledge that leaves with the person who
had it. `engineering/`, `roadmap/`, and `vision/` already hold real answers —
"why does auth use JWT", "what's the current architecture", "what's next" —
but nothing indexes them, so every question means grepping by hand or asking
whoever remembers.

The instinct is to solve this with an LLM. We're deliberately not doing that
first. If retrieval is bad, an LLM on top just produces confident wrong
answers faster. Version 1 has to prove the retrieval is good with no model to
hide behind.

## Proposal

### Vision

Engineering Memory turns a repo (or a set of repos) into a queryable index:
point it at `engineering/`, `roadmap/`, `vision/`, and itself, and get back
ranked, sourced answers to "what do we know about X" — as a CLI, before it's
anything else.

### Goals

- Index markdown, ADRs, and git metadata across one or more repos
- Full-text search with ranked, sourced results (file, score, related files)
- A retrieval path (`eng ask`) that answers a question by finding and
  bundling relevant documents — no generation, just better assembly than
  `eng search` alone
- CLI ergonomics good enough that engineers reach for it before grep
- A domain model and storage schema that Milestone 2 (ranking, relationships,
  semantic search) can extend without a rewrite

### Non-goals (v1)

Explicitly out of scope for this RFC and the resulting implementation:

- Embeddings, vector search, semantic ranking
- LLM/AI answers of any kind (no OpenAI, no Claude, no prompt building)
- MCP tools, agent integration, autonomous workflows
- Web UI, auth, multi-tenancy, distributed storage
- Ingesting anything other than markdown + git metadata (no PRs/issues yet —
  see Future work)

Every one of these is real future work. None of it is this RFC.

### Domain model

Full definitions live in [`DOMAIN_MODEL.md`](../docs/architecture/DOMAIN_MODEL.md) —
the v1-scoped slice of the general graph model in
[`KNOWLEDGE_MODEL.md`](../docs/architecture/KNOWLEDGE_MODEL.md). Summary of
entities and which ship in v1:

| Entity | v1 (MVP) | Later |
|--------|:--:|:--:|
| Workspace | ✅ | |
| Repository | ✅ | |
| Document | ✅ | |
| ADR | ✅ (as a Document subtype) | |
| Rule | partial (parsed, not enforced) | full enforcement — Milestone 2 |
| Decision | partial (derived from ADR) | first-class linking — Milestone 2 |
| Pull Request | | Milestone 2 |
| Issue | | Milestone 2 |
| Knowledge (derived cross-links) | | Milestone 2 |
| Component / Service | | Milestone 2 |
| Person | | Milestone 3+ |

### Architecture

Full component breakdown lives in [`ARCHITECTURE.md`](../docs/architecture/ARCHITECTURE.md).
Shape of the pipeline:

```
Repository → Parser → Indexer → Storage → Search → Retriever → CLI output
```

No component in this pipeline calls an LLM or an external API. Everything
runs locally against files already on disk.

### CLI

Seven commands for v0.1 (revised from an earlier five-command draft — `add`
and `doctor` earn their place once a Workspace spans multiple repos; see
[`CLI.md`](../docs/cli/CLI.md) for the full reconciliation):

```
eng init      # create a workspace + local SQLite index in the current repo
eng add PATH  # register another repository into the current workspace
eng index     # walk registered repos, parse docs, populate the index
eng search Q  # ranked full-text search, returns file + score + related files
eng ask Q     # retrieval bundle: relevant docs assembled by heuristics, no LLM
eng status    # index health: doc count, staleness, last index time
eng doctor    # diagnose a broken index (orphaned rows, missing repo paths)
```

Detailed usage, flags, and output shapes live in [`CLI.md`](../docs/cli/CLI.md) so
this RFC stays about scope, not syntax.

### Storage

SQLite, single file per workspace. Tables (names only — full schema is
[`DATABASE.md`](../docs/architecture/DATABASE.md)):

```
repositories · documents · document_chunks · tags · relationships · index_state
```

### MVP scope, restated

```
Repository → Index → Store → Search → Return Knowledge
```

If that loop works end-to-end on `engineering-kernel`, `engineering`, `roadmap`, and
`vision` themselves, v1 is done.

## Alternatives considered

- **Start with embeddings/semantic search.** Loses: no baseline to know if
  semantic search is even better than full-text for this corpus, and it adds
  a dependency (embedding model, vector store) before we've proven retrieval
  matters at all.
- **Adopt an existing tool (Notion AI, Glean, Sourcegraph, Obsidian + plugin).**
  Loses: none of them are git-native, agent-first, or embeddable as a CLI
  other tools (Cursor, CI, other agents) can shell out to. We'd be adapting
  our workflow to their model instead of the other way around.
- **Skip the kernel, build the AI layer directly on ad hoc file reads.**
  Loses: every future feature (review, planning, agents) re-implements its
  own retrieval, and we never get a shared, inspectable index to reason
  about or debug.
- **Do nothing — keep grepping and asking around.** Valid today, doesn't
  scale past a handful of repos or past the people who were there when the
  decision was made.

## Trade-offs & risks

- **SQLite ceiling.** Fine for single-machine, single-team scale; multi-repo
  fan-out or concurrent writers may need revisiting (tracked as an open
  question, not solved here).
- **Full-text search has a quality ceiling.** Some questions genuinely need
  semantic matching. We accept a worse-than-ideal `eng ask` in v1 in exchange
  for a system we can fully explain and debug — Milestone 2 adds ranking and
  relationships once we know where full-text actually falls short.
- **Risk of building infrastructure nobody uses.** Mitigated by dogfooding
  immediately on the four existing repos rather than a synthetic corpus —
  if `eng search` isn't useful on `engineering/ADR/`, we'll know in week one.
- **Risk of over-scoping the domain model early.** Mitigated by marking most
  entities "Later" in the table above and only implementing what v1 needs.

## Rollout

1. Land this RFC + `ARCHITECTURE.md` + `DOMAIN_MODEL.md` (Week 1, Mon–Tue).
2. Bootstrap the Go module and CLI skeleton, no logic (Week 1, Fri).
3. Implement parser → indexer → storage → search against `engineering-kernel/` itself
   (Sprint 2).
4. Point it at `engineering/`, `roadmap/`, `vision/` and dogfood daily.
5. Only after `eng search` and `eng ask` are trusted day-to-day does Milestone
   2 (relationships, ranking, semantic search) start.

## Open questions

- Single SQLite file per repo, or one workspace DB spanning all four repos
  from day one?
- How do we define a "chunk" for a markdown file — whole file, per heading,
  fixed-size window?
- Cross-repo document identity: path-based id, or content hash, once a
  workspace spans multiple repos?
- Is git metadata (commit history, blame, authorship) in v1 scope, or does
  `eng index` start markdown-only and git metadata lands with Milestone 2?
- Does `eng ask`'s "no LLM" retrieval bundle need its own ranking heuristic,
  or is it `eng search` with a different output formatter?
