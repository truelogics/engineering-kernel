---
rfc: 0002
title: Knowledge Engine
status: draft
author: founding-team
created: 2026-08-02
supersedes:
superseded_by:
resulting_adr:
---

# RFC 0002 — Knowledge Engine

## Summary

[ARCHITECTURE.md](../docs/architecture/ARCHITECTURE.md) and
[INTERFACES.md](../docs/architecture/INTERFACES.md) already describe *what*
the v1 pipeline is: `Source → Collector → Parser → Normalizer → Chunker →
Indexer → Storage → Search → Retriever → Context Builder`. This RFC is the
*why* — the argument for each stage existing as its own seam, and the
alternative each one beat. It's written after those docs, not before, because the shape
came first and earned scrutiny second; that scrutiny is recorded here so
the shape doesn't get quietly reopened later without someone re-deriving
this reasoning from scratch.

## Motivation

A pipeline with nine named seams is more ceremony than a script that reads
files and stuffs them into SQLite FTS. That script would work for v1. The
question this RFC answers is why we're not writing that script — why pay
for the seams now, before any of them have more than one implementation
behind them.

### Why a pipeline at all?

Because the alternative — one component that reads, parses, and stores in
one pass — can't add a second Source without a rewrite. `engineering-kernel`,
`engineering`, `roadmap`, and `vision` are the first four repos; Slack,
Jira, Notion, and GitHub's API are named future Sources in
[KNOWLEDGE_MODEL.md](../docs/architecture/KNOWLEDGE_MODEL.md). A monolith
means every one of those additions touches the same file that already
handles markdown. A pipeline means each addition is a new implementation of
one interface, plugged into stages that don't change.

### Why Collectors, separate from Parsers?

Because fetching bytes and interpreting bytes are different kinds of
failure. Fetching can be slow, rate-limited, or require auth — none of
which is true of parsing a byte slice already in memory. Keeping them
separate means `Parser` can be tested with no filesystem and no network at
all: feed it bytes, assert on the `Document`. If `Collector` and `Parser`
were one interface, every `Parser` test would need a real (or mocked) file
on disk, and every future `Collector` (an HTTP client with retries) would
drag its concerns into code that should only care about markdown syntax.

### Why Parsers per format, instead of one generic parser?

Because markdown, and later a PDF or a Slack thread's JSON, don't share
structure — a single parser handling all of them becomes a branch per
format, and a bug in the PDF branch risks the markdown branch it sits next
to. One `Parser` implementation per format means adding PDF support is
additive (a new implementation of the interface) rather than a modification
to code that markdown parsing already depends on.

### Why Normalization, when v1's is a pass-through?

Because `Chunker`, `Indexer`, `Storage`, and `Search` should never need to
know which `Parser` produced a `Document`. Today there's only one `Parser`
(markdown), so `Normalizer` has nothing to reconcile — but the alternative,
skipping `Normalizer` and letting each `Parser` produce whatever `Chunker`
happens to expect, means that contract is implicit and enforced by
convention. The first time a second `Parser`'s output shape drifts even
slightly, the bug shows up downstream in `Chunker` or `Search`, not at the
seam where it actually originated. Naming the seam now costs one trivial
pass-through implementation; not naming it costs a debugging session later,
in code that by then has forgotten this was ever a decision.

### Why Chunking?

Because `eng search "authentication"` matching `ARCHITECTURE.md` in its
entirety is a useless result compared to matching the three paragraphs that
are actually about authentication. Search relevance operates at whatever
granularity the indexed unit is — a document-level index can only ever say
"this file," never "this file, roughly here." Chunking is also a
prerequisite for embeddings (Milestone 2, per KNOWLEDGE_MODEL.md): a
100KB file isn't an embeddable unit, a chunk plausibly is. Deciding the
*existence* of chunking now means Milestone 2 extends `Chunker`'s strategy;
without this RFC's seam, Milestone 2 would be adding chunking from scratch
under time pressure, with less room to get the boundary right.

### Why Indexing, separate from Storage?

Because "what needs to be re-indexed" (skip unchanged files via
`content_hash`, decide what a full vs. incremental run touches) is a policy
decision, and "how a row gets written" (the actual SQL) is a mechanical
one. Folding `Indexer` into `Storage` means a "smart" storage layer that
both decides staleness and executes writes — harder to test in isolation
(you can't verify incremental-skip logic without a real database), and
harder to change independently (a new incremental strategy and a new
storage backend become the same PR by construction, even when they have
nothing to do with each other).

## Proposal

Adopt the pipeline as already specified: ten interfaces, defined with
responsibilities but no methods in
[INTERFACES.md](../docs/architecture/INTERFACES.md); components and
boundaries in
[ARCHITECTURE.md](../docs/architecture/ARCHITECTURE.md); the conceptual
justification for each stage as argued above. This RFC doesn't add new
scope — it ratifies the shape those two documents already describe, so that
shape has a recorded "why" instead of only a recorded "what."

## Alternatives considered

- **One monolithic ingest script** (read → parse → write SQLite FTS, no
  seams). Wins on v1 velocity. Loses the moment a second Source or a second
  format exists — which KNOWLEDGE_MODEL.md's Sources list says is not
  hypothetical, it's Milestone 2.
- **Merge Collector into Source.** Considered directly — v1's Collector is
  thin enough (`os.ReadFile`) that this is a live question, left open in
  INTERFACES.md rather than resolved here, since resolving it doesn't block
  anything: both shapes support the same v1 implementation.
- **Skip Normalizer entirely, formalize it only when a second Parser
  exists.** Rejected: the cost of naming the seam now (one pass-through
  implementation) is lower than the cost of retrofitting a contract onto
  code that's already been written against an implicit one.
- **Whole-document indexing, no Chunker.** Rejected per "Why Chunking?"
  above — blocks both useful search relevance and future embeddings.

## Trade-offs & risks

- **Nine interfaces for a v1 that has one Source and one Parser is more
  structure than the immediate problem needs.** Accepted deliberately: the
  cost is a handful of thin pass-through types, not months of work, and the
  payoff is specifically for the multi-Source future this whole project is
  named after (Engineering *Memory*, not "markdown search tool").
  [DOMAIN_MODEL.md](../docs/architecture/DOMAIN_MODEL.md)'s own discipline —
  "if a v1 task seems to need Person or Component, that's a sign the task
  belongs to a later milestone" — applies here too: the interfaces existing
  doesn't mean their non-trivial implementations get pulled forward.
- **Risk: interfaces designed before any method signature exists could be
  wrong in ways that only show up once Sprint 2 writes real code against
  them.** Mitigated by INTERFACES.md being explicitly unfinished (no
  methods) — the boundary (what each stage owns) is the part being locked
  in now; the contract (exact function signatures) stays open until
  implementation forces the question.

## Rollout

No rollout in the deployment sense — this is a design ratification, not a
change to running systems. Sprint 2 implements against these interfaces in
the order ARCHITECTURE.md's pipeline runs: `Parser` first (nothing works
without it), then `Indexer`/`Storage` together (they're each other's only
caller in v1), then `Search`, then `Retriever`. `Source` and `Collector`
get their real (if thin) v1 implementations alongside `Parser`, since
`eng index` can't run without them. `Normalizer`'s pass-through can be
written in an afternoon whenever it's convenient — nothing blocks on it.

## Open questions

Carried forward from INTERFACES.md, not re-litigated here:

- Does `Collector` end up merged into `Source` once method signatures make
  the split's cost/benefit concrete, rather than theoretical?
- Where do cross-cutting concerns (errors, logging, cancellation) attach
  across these ten interfaces?
- Does `Chunker` need a pluggable strategy per Knowledge Type from day one,
  or does v1 ship one strategy and revisit once ADRs and READMEs are shown
  to chunk poorly with the same rule?
