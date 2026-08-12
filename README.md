---
doc: README
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-12
---

# Engineering Kernel

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

A persistent context layer for agents. It ingests engineering docs, decisions,
and conventions so AI systems can remember, enforce, and assist across sessions.

## Install

Requires **Go 1.25+** and **git**. There are no prebuilt binaries, and
nothing to clone.

```bash
go install github.com/truelogics/engineering-kernel/cmd/eng@latest
export PATH="$(go env GOPATH)/bin:$PATH"   # in your shell profile, not just this session

eng setup ~/engineering-os \
  --rules git@github.com:truelogics/engineering.git \
  --repo  ~/code/your-application
```

Two commands. The second one creates the workspace, clones and indexes
what you named, installs `engineering-mcp` if it is missing, registers
it with Claude Code, and installs the `/review-branch` command. Then:

```bash
cd ~/code/your-application
eng doctor        # eight checks; the first ✘ is the cause, the rest are symptoms
claude            # then type: /review-branch
```

`--rules` and `--repo` take a local path or a git URL and may be
repeated. `eng setup` is re-runnable: run it again to add a repository,
or after rebuilding, to repoint Claude Code at the new binary.

**`--rules` is the one that matters.** It is whichever repository holds
your organization's rules, ADRs and standards — for this organization,
[`engineering`](../engineering/); for yours, perhaps a `docs` repo or a
handbook. Without it everything works and has nothing to say: a workspace
holding only your application answers every question about rules with a
confident "no rule governs this", which reads exactly like a correct
answer.

The long version, with what each step produces and what goes wrong, is
[`engineering-mcp/INSTALL.md`](../engineering-mcp/INSTALL.md).

## Use

A **workspace** is one index over one or more repositories — `eng setup`
created one for you. In any repository you work in:

```bash
eng taxonomy auto   # propose what this repository's directories mean, and ask
eng update          # incremental re-index after documents change
eng status          # what is indexed, and whether a rulebook is present
eng search "authentication"
eng ask "how do we handle permission caching?"
eng doctor          # check the whole machine, and say what to fix
eng review          # check the setup, then hand over to Claude Code
```

To add a repository later, either re-run `eng setup` with another
`--repo`, or attach it directly:

```bash
cd ~/engineering-os && eng workspace attach ~/code/another-application
```

Every command, with what each is for and how it fails, is in
[`docs/cli/CLI.md`](docs/cli/CLI.md). When something is wrong, run
`eng doctor` first — it checks the binaries, the workspace, the index,
retrieval, and the Claude Code side, and names the first thing that
is broken.

## Why does it exist?

Agents start fresh every session. Knowledge lives in scattered docs and people's
heads. Engineering Kernel turns `engineering/` (and related repos) into queryable memory
agents can load on demand.

## Who is it for?

- Agents that need durable context (review, codegen, onboarding)
- Engineers building or integrating agent tooling
- Anyone defining how memory is ingested and queried

## Current status

**In daily use.** The whole pipeline runs end-to-end against real
repositories — filesystem collection, goldmark markdown parsing, SQLite
with FTS5, retrieval and context assembly, all wired through
`internal/indexer`. Two consumers build on it: `engineering-review` and
`engineering-mcp`. No AI, no embeddings, no vector database, by design
(RFC-0001's non-goals).

The command surface is [`CLI.md`](docs/cli/CLI.md). `eng ask` and `eng
doctor` ship; `eng add` was replaced by `eng workspace attach`. An
earlier version of this paragraph described all three as designed but
unimplemented, and stayed that way for several sprints after they
weren't — which is why `CLI.md` is now written from the binary rather
than from a plan.

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

Code dirs: `cmd/`, `internal/` and `pkg/` are implemented (see
[`internal/README.md`](internal/README.md) for the package map);
`tests/` stays reserved — see its README for why.

## Related repos

| Repo | Role |
|------|------|
| [`engineering/`](../engineering/) | Source docs & rules consumed by memory |
| [`roadmap/`](../roadmap/) | Company priorities |
| [`vision/`](../vision/) | Company north star |

## Map

```
engineering-kernel/
├── README.md
├── LICENSE
├── CHANGELOG.md
├── CONTRIBUTING.md
├── go.mod
├── rfcs/               ← design proposals (0001 kernel … 0009 taxonomy proposal)
├── docs/
│   ├── architecture/   ← KNOWLEDGE_MODEL.md, ARCHITECTURE.md, DOMAIN_MODEL.md, DATABASE.md, INTERFACES.md, GRAPH.md
│   ├── cli/            ← CLI.md
│   └── api/ storage/ search/ sdk/ plugins/ examples/   ← reserved
├── cmd/eng/            ← the Engineering OS shell (RFC-0008)
├── internal/           ← implemented — see internal/README.md for the map
├── pkg/memory/         ← the public SDK (RFC-0004) — what consumers build on
├── examples/           ← reserved — runnable usage examples
├── scripts/            ← reserved — dev/build scripts
└── tests/              ← reserved by choice — see tests/README.md
```
