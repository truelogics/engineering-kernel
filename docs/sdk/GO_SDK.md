---
doc: GO_SDK
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Go SDK — `pkg/memory`

> Companion to [RFC-0004](../../rfcs/0004-public-sdk.md). This is the only
> supported way for code outside this module to use Engineering Kernel. Everything
> under `internal/` — `Collector`, `Parser`, `Chunker`, `Indexer`,
> `Search`, `Retriever`, `Storage` — is off limits to importers; Go's
> compiler enforces it, and this package is the boundary it enforces it
> behind.

## Why this exists

`engineering-review` — the first real consumer built against this kernel — could
not contain a single line of Go code, because every package it needed
lived under `internal/`. That's the whole reason `pkg/memory` exists: not
a style preference, a blocking bug found by trying to build a consumer
instead of guessing what one would need. See
`engineering-review/KERNEL_REQUIREMENTS.md` gaps #1–#3.

## Install

```
go get github.com/truelogics/engineering-kernel/pkg/memory
```

## The five operations

`pkg/memory` is deliberately small. It has one type, `Memory`, and these
methods:

```go
func Open(workspaceDir string) (*Memory, error)
func (m *Memory) Close() error
func (m *Memory) AddRepository(ctx context.Context, path string) (Repository, error)
func (m *Memory) Index(ctx context.Context, repo Repository) (IndexResult, error)
func (m *Memory) Sync(ctx context.Context, repo Repository) (IndexResult, error)
func (m *Memory) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
func (m *Memory) Context(ctx context.Context, task string) (ContextPackage, error)
```

There is no separate `Workspace` type. `*Memory` **is** the open
workspace handle — `Open` returns it, `AddRepository` is a method on it.
Nothing else in this package is exported beyond the types these methods
take and return.

### `Open` — open or create a workspace

```go
m, err := memory.Open("./")
if err != nil {
    log.Fatal(err)
}
defer m.Close()
```

Creates `<workspaceDir>/.eng/memory.db` if it doesn't already exist — the
same convention `eng init` uses. One `Memory` per workspace directory;
opening the same directory twice from two processes is not supported (one
SQLite file, no locking coordination beyond what SQLite itself provides).

### `AddRepository` — register a repository to index

```go
repo, err := m.AddRepository(ctx, "/path/to/some/repo")
```

Idempotent: calling it again with the same path returns the existing
registration rather than erroring or duplicating it. `Repository` is a
narrow public DTO:

```go
type Repository struct {
    ID        string
    Name      string
    LocalPath string
    RemoteURL string // best-effort; empty if the path has no git remote
}
```

### `Index` / `Sync` — populate the workspace

```go
result, err := m.Index(ctx, repo)   // full walk
result, err := m.Sync(ctx, repo)    // incremental, via git diff
```

Both return an `IndexResult`:

```go
type IndexResult struct {
    Scanned, Added, Updated, Unchanged, Deleted, Errors int
}
```

`Sync` falls back to a full `Index` when `repo` has never been indexed or
isn't a git repository — callers don't need to know which happened; the
counts in `IndexResult` describe what actually ran either way.

### `Search` — ranked full-text query

```go
hits, err := m.Search(ctx, "authentication", memory.SearchOptions{Limit: 10})
```

```go
type SearchOptions struct {
    RepositoryID string // empty searches every registered repository
    Limit        int    // 0 uses the kernel's default
}

type SearchResult struct {
    Path    string
    Score   float64
    Snippet string
    Related []string // paths of other documents connected to this one
}
```

Hybrid ranking (keyword + relationship-graph connectivity, and embeddings
when a provider is configured) — see `docs/architecture/ARCHITECTURE.md`.
Not configurable through this API yet; `internal/search.RankWeights` stays
an internal concern until a consumer asks to tune it.

### `Context` — structured context for a task

```go
ctx, err := m.Context(ctx, "Review authentication PR")
```

```go
type ContextPackage struct {
    Task          string
    RelevantFiles []FileContext // architecture, documentation, RFCs, roadmap
    ADRs          []FileContext
    Rules         []FileContext
    RelatedIssues []FileContext // always present, always empty — no Issue ingestion yet
    RelatedPRs    []FileContext // always present, always empty — no PR ingestion yet
}

type FileContext struct {
    Path    string
    Score   float64
    Snippet string
}
```

This is the structured replacement for `eng context`'s flat terminal text,
meant for a program to consume directly rather than parse. It deliberately
has no `SimilarCode` or `Risks` field — this kernel has never indexed
source code, and risk assessment is a reasoning task for a consumer's LLM
layer, not something retrieval produces. See RFC-0004's Proposal section
for the full reasoning.

## What's not here

- No HTTP API — one process, one consumer so far; see RFC-0004's
  Alternatives section for why that's deferred, not rejected.
- No ranking/weight tuning, no version/compatibility policy yet — open
  questions in RFC-0004, left for whenever a real consumer asks.
- `internal/cli` does not yet call this package — it still wires
  `internal/*` directly. See RFC-0004's Rollout section for why that
  refactor was deferred rather than done alongside this one.
