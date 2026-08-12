---
rfc: 0004
title: "Public SDK: pkg/memory, Context Package, Workspace"
status: draft
author: founding-team
created: 2026-08-02
supersedes:
superseded_by:
resulting_adr:
---

# RFC 0004 — Public SDK: pkg/memory, Context Package, Workspace

## Summary

Engineering Kernel Sprint 3, scoped to exactly the three gaps `engineering-review/KERNEL_REQUIREMENTS.md`
marked as blocking any consumer from existing at all: a public Go API
(`pkg/memory`), a structured context contract (`ContextPackage`, replacing
`AssembledContext`'s flat text for programmatic consumers), and a public
`Workspace`/`Repository` API. Everything else `engineering-review` found (path-glob
rule scoping, PR ingestion, ownership, API versioning, ...) stays
explicitly out of scope — this RFC is P0 only, per the CTO review's own
classification.

## Motivation

Every package this kernel needs to expose
(`internal/kernel`, `internal/search`, `internal/retriever`,
`internal/contextbuilder`, `internal/indexer`) lives under `internal/`.
Go's own compiler enforces that nothing outside this module — including
`engineering-review`, sitting right next to it — can import any of it. This
isn't a style preference to fix eventually; it's the literal reason
`engineering-review` could not contain a single line of Go code today, discovered
by trying to design one instead of guessing it might matter.

## Proposal

### `pkg/memory` — one facade, narrow on purpose

```go
type Memory struct { /* unexported: storage, indexer, search, retriever, contextBuilder */ }

func Open(workspaceDir string) (*Memory, error)
func (m *Memory) Close() error
func (m *Memory) AddRepository(ctx context.Context, path string) (Repository, error)
func (m *Memory) Index(ctx context.Context, repo Repository) (IndexResult, error)
func (m *Memory) Sync(ctx context.Context, repo Repository) (IndexResult, error)
func (m *Memory) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
func (m *Memory) Context(ctx context.Context, task string) (ContextPackage, error)
```

Not "promote `internal/kernel` to `pkg/`" — a deliberately small surface
with its own types (`Repository`, `SearchOptions`, `SearchResult`,
`ContextPackage`), converted to/from `internal/domain`/`internal/kernel`
types inside `pkg/memory`'s implementation. This is the actual point:
`internal/kernel`'s shape stays free to change (it already has, six times,
across Milestones 1-7) without that becoming a breaking change for anyone
depending on `pkg/memory` — the conversion layer absorbs it.

**One entry point, not three constructors.** Today, `internal/cli`
independently wires `Collector`+`Parser`+`Normalizer`+`Chunker`+`Indexer`+
`Storage`+`Graph`+`Search`+`Retriever`+`ContextBuilder` inside each of its
five functions. `pkg/memory.Open` does this wiring exactly once; a
consumer never sees `Collector` or `Chunker` exist. `internal/cli` itself
gets refactored to call `pkg/memory` instead of duplicating the wiring —
proving the facade is sufficient by making the CLI its first real
consumer, not just its intended one.

**No separate `Workspace` type.** The CTO review's sketch
(`workspace := memory.Open("./")`) and this RFC's `*Memory` are the same
thing — a `Workspace` type distinct from `Memory` would be two names for
one concept. `*Memory` *is* the open workspace handle; `AddRepository` is
a method on it.

### `Repository` — a public DTO, not `domain.Repository` re-exported

```go
type Repository struct {
	ID        string
	Name      string
	LocalPath string
	RemoteURL string
}
```

Deliberately narrower than `internal/domain.Repository` (no
`LastIndexedCommit` — that's an implementation detail of incremental
sync, not something a consumer should read or set directly).

### `ContextPackage` — the structured contract, replacing flat text for programmatic use

```go
type ContextPackage struct {
	Task          string
	RelevantFiles []FileContext // architecture, documentation, RFCs, roadmap — everything not ADR/Rule
	ADRs          []FileContext
	Rules         []FileContext
	RelatedIssues []FileContext // always present, always empty — no Issue ingestion (RFC-0001)
	RelatedPRs    []FileContext // always present, always empty — no PR ingestion (RFC-0001)
}

type FileContext struct {
	Path    string
	Score   float64
	Snippet string
}
```

**`SimilarCode` and `Risks`, named in the CTO review's own sketch, are
deliberately not included.** Not an oversight — a boundary worth stating
explicitly:

- `SimilarCode` would require indexing and searching actual source code.
  Engineering Kernel has only ever parsed markdown (RFC-0001's non-goals, still
  true) — there is no code index to search. Adding an always-empty
  `SimilarCode` field would blur "not implemented" with "checked, found
  none," which this project has consistently avoided (see
  `RelatedIssues`/`RelatedPRs`'s pattern: those are empty because
  ingestion doesn't exist *yet*, a real future capability; `SimilarCode`
  would be empty because this kernel has never had a reason to parse code
  at all — a materially different kind of gap, deserving its own RFC if
  and when `engineering-review` actually needs it).
- `Risks` is a reasoning output (an LLM's job, per `engineering-review/ARCHITECTURE.md`'s
  own Prompt Builder/LLM split), not a retrieval output. Putting it on
  `ContextPackage` would mean either faking it or quietly calling a model
  from inside what's supposed to be a deterministic, local retrieval call
  — exactly the boundary RFC-0001 drew between the kernel and Milestone 3's
  eventual AI layer.

`RelevantFiles` folds `Retriever`'s Architecture/Documentation/RFCs/Roadmap/
Other groups together — `engineering-review`'s design doesn't (yet) need those
distinguished from each other the way ADRs and Rules specifically are.

## Alternatives considered

- **Promote `internal/kernel` types directly to `pkg/`.** Rejected: every
  kernel type has changed at least once per milestone since Step 7; making
  them public directly makes every future kernel change a potential
  breaking change for `engineering-review` (and anything after it), for no benefit
  over a thin conversion layer.
- **Skip the facade, let `engineering-review` import individual `internal/`
  packages via a `replace` directive hack or by vendoring.** Rejected —
  this is exactly the workaround that makes "internal" meaningless and
  defeats the entire reason Go's visibility rule exists.
- **Build the HTTP API (Milestone 10) instead of a Go SDK.** Rejected per
  the CTO review's own reasoning: one process, one consumer, no reason to
  pay for a network hop and serialization boundary yet.

## Trade-offs & risks

- **`pkg/memory` becomes an immediate compatibility commitment.** The
  moment `engineering-review` (or the refactored `internal/cli`) depends on it,
  changing its shape is a breaking change. Keeping it deliberately narrow
  (five methods, four types) is the mitigation — the smaller the surface,
  the cheaper it is to keep stable.
- **`ContextPackage`'s flattening loses some structure `RetrievalBundle`
  has** (e.g. which specific Knowledge Type a "relevant file" came from,
  beyond ADR/Rule). Acceptable for now — `engineering-review`'s design doesn't ask
  for that distinction; it can be added later without breaking existing
  fields if it turns out to matter.

## Rollout

1. `pkg/memory` (Milestone 1) — the facade and its wiring.
2. `ContextPackage` (Milestone 2) — folded into the same package, since it's
   `Memory.Context`'s return type; not meaningfully separable from
   Milestone 1's work.
3. `Repository`/workspace API (Milestone 3) — same package again; listed
   as its own milestone because the CTO review scoped it that way, but
   implemented alongside 1-2 since `AddRepository` is part of the same
   `Memory` type.
4. `docs/sdk/GO_SDK.md` — documents exactly `Open`/`Index`/`Sync`/`Search`/
   `Context`/`Close`/`AddRepository`, nothing else, per the CTO review's
   explicit instruction to keep the public surface's documentation as
   narrow as the surface itself.
5. Stop. Per the CTO review: no further kernel work until `engineering-review`'s
   actual implementation exercises this surface and finds the next real
   gap.

**Deferred, not done in this pass: refactoring `internal/cli` to call
`pkg/memory`.** Considered, but it isn't part of what Sprint 3 actually
asked for (three milestones plus `GO_SDK.md`, then stop), and it isn't
free: `eng context`'s current output has a separate line per
Retriever label (`Architecture:`, `Related ADRs:`, ...); `ContextPackage`
deliberately flattens most of those into `RelevantFiles` (this RFC's own
documented trade-off), so routing `eng context` through it would visibly
regress the CLI. `AddRepository` also doesn't report whether a repository
already existed, which `eng init`'s "Already registered" message depends
on. Neither is a reason to change `pkg/memory`'s shape now — they're
reasons `internal/cli` stays on its own wiring until a real need forces
the question, same discipline as everything else deferred out of this
RFC.

## Open questions

- Should `ContextPackage` eventually preserve `Retriever`'s full group
  labels (Architecture vs. Documentation vs. RFCs vs. Roadmap) instead of
  flattening them into `RelevantFiles`, once a consumer actually asks for
  that distinction?
- `Search.Weights`/`Retriever.Priority` (Milestone 7) aren't exposed
  through `pkg/memory.SearchOptions` in this pass — should tuning ranking
  be part of the public API, or stay an internal concern `engineering-review`
  never needs to touch? Left unresolved; add if `engineering-review` asks for it.
- `pkg/memory` has no version tag or compatibility policy yet (gap #11 in
  `engineering-review/KERNEL_REQUIREMENTS.md`) — deliberately deferred past this
  RFC, since it matters more once there's a second consumer than while
  there's a facade with zero external dependents.
