---
doc: SPRINT_2_REVIEW
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Sprint 2 Review — Step 7, Kernel MVP

Ran as one continuous session rather than ten separate branches/PRs (see
"Deviation from plan" below). This is the review artifact the plan called
for regardless of how it got built.

## Definition of Done: met

```
eng init
eng index .
eng search architecture
eng search authentication
eng status
```

All five run correctly against a real directory containing markdown
docs — verified both as an automated test
(`internal/cli`'s `TestDefinitionOfDone`) and as a literal manual run of
the compiled binary. No AI, no embeddings, no vector database, per
RFC-0001's non-goals.

## Milestones

| # | Milestone | Status | Package |
|---|-----------|--------|---------|
| 1 | Core Models | ✅ | `internal/domain` |
| 2 | Kernel Interfaces | ✅ | `internal/kernel` |
| 3 | SQLite Storage | ✅ | `internal/storage/sqlite` |
| 4 | Filesystem Collector | ✅ | `internal/collector/filesystem` |
| 5 | Markdown Parser | ✅ | `internal/parser/markdown` |
| 6 | Normalizer | ✅ | `internal/normalizer` |
| 7 | Chunker | ✅ | `internal/chunker` |
| 8 | Indexer | ✅ | `internal/indexer` |
| 9 | Search | ✅ | `internal/search` |
| 10 | Integration | ✅ | `internal/cli`, `internal/indexer` (end-to-end tests) |

## Test coverage

| Package | Coverage | Note |
|---|---|---|
| `internal/domain` | 100.0% | Pure validation logic, no I/O |
| `internal/normalizer` | 97.8% | |
| `internal/collector/filesystem` | 89.7% | |
| `internal/chunker` | 88.8% | |
| `internal/parser/markdown` | 88.7% | |
| `internal/search` | 80.5% | |
| `internal/cli` | 77.8% | |
| `internal/indexer` | 77.2% | |
| `internal/storage/sqlite` | 73.2% | |

**Honest gap against the plan's >90% target:** four packages land in the
73–81% range. The uncovered lines are almost entirely database-error and
OS-error branches (a closed connection mid-transaction, a permission-denied
file read) that need fault injection to reach cleanly — I judged that not
worth contorting tests for right now rather than padding the number. `go
vet ./...` and `go build ./...` are clean; every test that exists passes,
including one true end-to-end run of the real pipeline
(`internal/indexer`'s `TestIndexEndToEndWithRealComponents`) and one true
end-to-end run of the CLI (`internal/cli`'s `TestDefinitionOfDone`).

## Real decisions made during implementation

Things the design docs left open, resolved while writing code — flagged
here rather than silently baked in, per this repo's own convention:

- **`Source` stayed a domain model, not a live interface.** INTERFACES.md
  flagged this as an open question (Collector vs. Source splitting might
  be over-abstraction for v1's single Source type). Resolved: `Source` has
  no runtime role yet; `Collector` takes a `domain.Repository` directly.
  `CanonicalDocument.SourceID` is populated with the Repository's ID as a
  stand-in, since there's no persisted `sources` table.
- **Document identity is path-based** (`sha256(repositoryID, path)`), not
  content-hash-based — DATABASE.md's open question, resolved in
  `domain.NewCanonicalDocument`. A renamed file becomes a new row; an
  edited file keeps its row and updates in place.
- **`Chunker`'s heading strategy splits on every heading, any level** — no
  absorption of sub-headings into their parent section. Simpler and fully
  tested; a coarser strategy is explicitly left as future work (see
  `internal/chunker`'s doc comment).
- **`Search`'s "related documents" falls back to shared Tags** when no
  explicit Relationship exists — which is the common case in v1, since
  nothing yet auto-populates Relationships from body content (only an
  ADR's `supersedes:` front-matter would, and none of this repo's real
  RFCs have that field set yet). Structural stat Tags (`heading_count`,
  etc.) are excluded from that fallback since they'd match almost every
  document.
- **`kernel.Storage` grew one method beyond its Step 6 design**:
  `FindDocumentsByTag`, needed for the shared-tag fallback above. Added
  directly to `internal/kernel/storage.go` and documented in
  `INTERFACES.md` rather than treated as out-of-band.

## Known gaps, not hidden

- `eng add`, `eng ask`, `eng doctor` are unimplemented — `cmd/eng` prints
  "not yet implemented" and exits non-zero. `Retriever` and
  `ContextBuilder` exist only as interfaces (`internal/kernel`).
- v1 doesn't detect deleted files — `eng index` re-scans and upserts, but
  a file removed from disk keeps its old row and stays searchable.
- BM25 scores read near-zero on very small corpora (few documents sharing
  a term) — a statistical property of BM25's IDF term at low N, not a
  bug; relative ranking is still correct. Worth knowing before someone
  reads a `0.00` in `eng search` output and assumes something's broken.
- No incremental indexing beyond content-hash comparison — `eng index`
  always walks every file; skipping unchanged files by mtime before even
  reading them is a possible speedup, not implemented.
- One connection, `SetMaxOpenConns(1)` — correct for v1's single-process
  CLI, would need revisiting for any future concurrent-writer scenario.

## Deviation from plan: branch-per-milestone

The plan asked for one branch and one PR per milestone. This session built
all ten in one continuous pass on the working tree, uncommitted, per an
explicit instruction not to commit or push. That's a real deviation worth
naming rather than quietly reconciling: the milestones are cleanly
separable by package (each has its own directory and test file), so
splitting this into ten commits/branches after the fact — before ever
pushing — is mechanical, not risky. Flagging it as a decision point rather
than assuming which you'd want.
