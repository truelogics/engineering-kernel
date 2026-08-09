---
doc: README
audience: [human, agent]
status: living
owner: ai-memory
last_reviewed: 2026-08-02
---

# AI Memory

## What is this?

The kernel of the [AI Engineering OS](../vision/README.md). Mission: store,
organize, retrieve, and connect engineering knowledge. Everything else in the
OS (context, intelligence, workflows) gets built on top of this, the same way
an OS grows outward from a kernel — it doesn't start as one.

It also ships `eng`, which is the **shell of the Engineering OS**
(`engineering/RFC-0008-eng-cli.md`) rather than this repository's CLI. `eng`
coordinates; where a capability lives elsewhere, it delegates — `eng doctor`
runs `engineering-mcp doctor`, `eng review` hands over to Claude Code. A
developer should never need to know which repository answers a question.

```bash
eng init          # create a workspace here
eng taxonomy      # what this repository's directories mean
eng index         # index every repository in the workspace
eng doctor        # check this machine end to end
eng review        # check the setup, then hand over to Claude Code
```

A persistent context layer for agents. It ingests engineering docs, decisions,
and conventions so AI systems can remember, enforce, and assist across sessions.

## Why does it exist?

Agents start fresh every session. Knowledge lives in scattered docs and people's
heads. AI Memory turns `engineering/` (and related repos) into queryable memory
agents can load on demand.

## Who is it for?

- Agents that need durable context (review, codegen, onboarding)
- Engineers building or integrating agent tooling
- Anyone defining how memory is ingested and queried

## Current status

**Kernel MVP working.** `eng init`, `eng index`, `eng search`, and `eng
status` run end-to-end against real markdown repos — filesystem
collection, goldmark-based markdown parsing, SQLite storage with FTS5
search, all wired through `internal/indexer`. No AI, no embeddings, no
vector database (by design — see RFC-0001's non-goals). `eng add`, `eng
ask`, and `eng doctor` are designed ([`CLI.md`](docs/cli/CLI.md)) but not
implemented; `Retriever`/`Context Builder` exist only as interfaces. See
[`SPRINT_2_REVIEW.md`](SPRINT_2_REVIEW.md) for the milestone-by-milestone
breakdown, test coverage, and known gaps.

## Roadmap

This repo has no roadmap file of its own — company-wide priority lives in
[`roadmap/NOW.md`](../roadmap/NOW.md) and milestones in
[`roadmap/MILESTONES.md`](../roadmap/MILESTONES.md), so there's exactly one
place to check what's next, not two that can drift out of sync.

## Contributing

1. Read [`RFC-0001`](rfcs/0001-engineering-memory-kernel.md),
   [`RFC-0002`](rfcs/0002-knowledge-engine.md),
   [`RFC-0003`](rfcs/0003-engineering-intelligence.md),
   [`KNOWLEDGE_MODEL.md`](docs/architecture/KNOWLEDGE_MODEL.md) (start here —
   what Engineering Knowledge actually is),
   [`ARCHITECTURE.md`](docs/architecture/ARCHITECTURE.md),
   [`DOMAIN_MODEL.md`](docs/architecture/DOMAIN_MODEL.md),
   [`DATABASE.md`](docs/architecture/DATABASE.md),
   [`INTERFACES.md`](docs/architecture/INTERFACES.md),
   [`GRAPH.md`](docs/architecture/GRAPH.md) (design only, Step 8 not yet
   implemented), and
   [`CLI.md`](docs/cli/CLI.md)
2. Significant design → open an RFC under `rfcs/` (start from `0000-template.md`)
3. Org-wide decisions also land in [`engineering/ADR/`](../engineering/ADR/)

Code dirs: `cmd/` and `internal/` are implemented (see
[`internal/README.md`](internal/README.md) for the package map); `pkg/`
and `tests/` stay reserved — see their own READMEs for why.

## Related repos

| Repo | Role |
|------|------|
| [`engineering/`](../engineering/) | Source docs & rules consumed by memory |
| [`roadmap/`](../roadmap/) | Company priorities |
| [`vision/`](../vision/) | Company north star |

## Map

```
ai-memory/
├── README.md
├── LICENSE
├── CHANGELOG.md
├── CONTRIBUTING.md
├── go.mod
├── rfcs/               ← design proposals (0001: Engineering Memory Kernel)
├── docs/
│   ├── architecture/   ← KNOWLEDGE_MODEL.md, ARCHITECTURE.md, DOMAIN_MODEL.md, DATABASE.md, INTERFACES.md, GRAPH.md
│   ├── cli/            ← CLI.md
│   └── api/ storage/ search/ sdk/ plugins/ examples/   ← reserved
├── cmd/eng/            ← CLI entrypoint: init, index, search, status (real);
│                          add, ask, doctor (not yet implemented)
├── internal/           ← implemented — see internal/README.md for the map
├── pkg/                ← reserved — public libraries
├── examples/           ← reserved — runnable usage examples
├── scripts/            ← reserved — dev/build scripts
└── tests/              ← reserved by choice — see tests/README.md
```
