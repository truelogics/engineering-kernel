---
doc: CONTRIBUTING
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Contributing

How to propose and land changes to Engineering Memory.

## Decision flow

1. **RFC** — propose significant or contested design changes in
   [`rfcs/`](rfcs/), starting from `rfcs/0000-template.md`. See
   [`rfcs/0001-engineering-memory-kernel.md`](rfcs/0001-engineering-memory-kernel.md)
   for what a filled-in RFC looks like.
2. **Update `docs/`** — once an RFC is accepted, the relevant `docs/`
   subfolder ([`architecture/`](docs/architecture/), [`cli/`](docs/cli/), …)
   is what stays current day-to-day; the RFC itself doesn't get edited after
   acceptance.
3. **Org-wide decisions** (ones that affect more than this repo) also get an
   ADR in [`engineering/ADR/`](../engineering/ADR/).

Trivial edits (typos, clarifications) can skip the RFC — open a PR directly.

## Code of conduct

Follow [`engineering/CODE_OF_CONDUCT.md`](../engineering/CODE_OF_CONDUCT.md) —
this repo doesn't keep its own copy.

## Conventions

- Every `.md` file starts with YAML front-matter (`doc`, `status`, `owner`,
  `last_reviewed`).
- A folder with no content yet gets a one-line `README.md` marked
  `status: reserved` — explaining what will live there, not placeholder prose.
- `cmd/`, `internal/`, `pkg/`, `tests/` hold implementation — see
  [`docs/architecture/ARCHITECTURE.md`](docs/architecture/ARCHITECTURE.md)
  for the component boundaries before adding code to any of them.

## What v1 is not

Before proposing a change, check
[`rfcs/0001-engineering-memory-kernel.md`](rfcs/0001-engineering-memory-kernel.md)'s
non-goals: no embeddings, no LLM/AI answers, no MCP or agent integration, no
web UI, no PR/issue ingestion. Those are real future milestones, not this
repo's current scope — proposals for them belong in a new RFC, not a patch
to v1's implementation.
