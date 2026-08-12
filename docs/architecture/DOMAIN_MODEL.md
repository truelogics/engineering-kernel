---
doc: DOMAIN_MODEL
audience: [human, agent]
status: draft
owner: engineering-kernel
last_reviewed: 2026-08-01
---

# Domain model

> Companion to [RFC-0001](../../rfcs/0001-engineering-memory-kernel.md). This
> is the business-level model — what Engineering Memory is *about* — not a
> database schema. Storage-level shape (columns, types) is
> [`DATABASE.md`](DATABASE.md). This is the **v1-scoped** slice of the full
> shape in [`KNOWLEDGE_MODEL.md`](KNOWLEDGE_MODEL.md) — read that first for
> how ADR/Rule/Decision/Component/Service map onto the general Document/Entity
> distinction; this file stays the concrete, implementation-facing list.

## Entities

### Workspace

The scope of one Engineering Memory index — one or more Repositories indexed
together (e.g. `engineering-kernel` + `engineering` + `roadmap` + `vision` as a single
workspace). `eng init` creates one.

### Repository

A git repo registered in a Workspace. Tracked: remote URL, local path, last
indexed commit.

### Document

A source file Engineering Memory has parsed — markdown with front-matter,
belonging to a Repository. Every other content entity (ADR, Rule, Decision)
is a Document with a more specific role, not a separate storage concept.

### ADR

A Document whose `doc:` front-matter marks it as an architectural decision
record. Carries a number, status (`proposed`/`accepted`/`deprecated`/…), and
is immutable once accepted — amendments are new ADRs that supersede it, not
edits.

### Rule

A machine-checkable directive: severity, glob pattern it applies to, and the
imperative sentence itself. Usually extracted from a Document in
`engineering/rules/` or an inline `rules` block. v1 parses and stores rules;
it does not enforce them (enforcement is a consumer's job, e.g. Engineering Review —
Milestone 4).

### Decision

The resolution captured by an ADR: context, decision, consequences. In v1
this is derived directly from an ADR Document rather than modeled as an
independent row — see Open questions in RFC-0001 on whether that changes once
Decisions need to link to non-ADR sources (e.g. an RFC that never became an
ADR).

### Pull Request *(Milestone 2)*

A merged or open PR from a Repository's forge (GitHub). Not ingested in v1 —
see RFC-0001 non-goals. Modeled now because Document ↔ PR links (an ADR
referencing "Related PRs") are already part of the v1 output shape in
`eng ask`, even before PRs themselves are indexed.

### Issue *(Milestone 2)*

Same status as Pull Request: modeled for future linkage, not ingested yet.

### Knowledge *(Milestone 2)*

Not a stored entity — the derived graph of relationships between Documents,
ADRs, Rules, PRs, and Issues once cross-linking exists. Named here because it
is the connective concept the whole system is building toward; v1 only lays
the groundwork (the `relationships` table exists in storage, mostly unused).

### Component / Service *(Milestone 2)*

The system/service a Document, ADR, or Rule is about (e.g. "auth service").
Lets a future query be "what do we know about the auth service" rather than
only "what do we know about this file." Deferred because it requires either
manual tagging conventions or inference neither of which is designed yet.

### Person *(later, Milestone 3+)*

Who owns or authored something. Deferred: git author metadata is captured
opportunistically in v1 (see ARCHITECTURE.md's Indexer), but Person as a
first-class entity (ownership queries, "who should review this") is out of
scope until the system has consumers that need it.

## Relationships

```
Workspace 1──* Repository

Repository 1──* Document
Document   1──1 ADR            (an ADR is a Document, not a separate table)
Document   1──* Rule           (rules extracted from a doc)
Document   1──1 Decision       (for ADR-shaped docs)
Document   *──* Document       (cross-links: "related files")

Document   *──* PullRequest    (Milestone 2)
Document   *──* Issue          (Milestone 2)
Document   *──* Component      (Milestone 2)
Component  *──* Service        (Milestone 2)
Document   *──1 Person         (author — Milestone 3+)
```

## What v1 actually touches

Of the entities above, v0.1 implements: **Workspace, Repository, Document,
ADR, Rule (parse-only), Decision (derived)**. Everything marked Milestone
2/3+ exists in this document so the shape doesn't need to be redesigned later
— but no table, parser, or CLI output should be built for them yet. If a v1
task seems to need Person or Component, that's a sign the task belongs to a
later milestone, not that the entity should be pulled forward.

## Open questions

Carried from RFC-0001, restated here since they're domain questions as much
as storage ones:

- Does Decision stay derived-from-ADR, or become independent once RFCs
  (which may never become ADRs) also need to resolve to a Decision?
- ~~Is a Document's identity its repo-relative path, or a content hash~~ —
  **resolved: path-based**, `sha256(repositoryID, path)` (Step 7,
  `domain.NewCanonicalDocument`). Still an open trade-off across repos in
  a workspace: the same conceptual doc moving repos gets a new id.
- Where does a Rule "live" once `engineering/rules/*.yaml` isn't the only
  source of rules — does Rule stay a child of Document, or need its own
  provenance model?
