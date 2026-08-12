---
doc: README
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-13
---

# Engineering Kernel

**Your team writes down how you build software — coding rules, decisions,
architecture notes. This makes an AI assistant actually read them.**

Ask Claude Code to review your branch and it reviews it with general
programming knowledge. It does not know your team decided to stop using
that library last March, or that you have a rule about how errors are
wrapped. Those things are written down. Nothing reads them.

Engineering Kernel reads them. It builds a searchable index of your
team's documents, and gives your AI assistant a way to look things up in
it — so a review can say *"this breaks `engineering:rules/logging.md`"*
and quote the line.

You need two things: **your code**, and **a repository with your team's
rules and decisions in it**. Both are just folders of markdown and code
on your machine.

---

## Setup

About five minutes. You need [Go 1.25+](https://go.dev/dl/), git, and
[Claude Code](https://claude.com/claude-code). There is nothing to clone.

### Step 1 — Install the `eng` command

```bash
go install github.com/truelogics/engineering-kernel/cmd/eng@latest
```

Go puts it in `$(go env GOPATH)/bin`, which is usually **not** on your
`PATH`. Add it, and put this line in your `~/.zshrc` or `~/.bashrc` so it
survives closing the terminal:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Check it worked:

```bash
eng version        # → eng version v0.3.0
```

If you get `command not found`, the `PATH` line above is the reason.

### Step 2 — Run setup

This is the only setup command. Replace the three placeholder paths with
your own:

```bash
eng setup ~/engineering-os \
  --rules ~/code/your-team-rules-repo \
  --repo  ~/code/your-application
```

| What you write | What it means |
|---|---|
| `~/engineering-os` | A **new, empty folder** for Engineering OS to keep its index in. It creates it. Pick anywhere; this path is a fine default. |
| `--rules <path>` | The folder holding **your team's rules, decisions and standards**. Often a `docs` repo, a handbook, or an `engineering` repo. |
| `--repo <path>` | The folder holding **the code you want reviewed**. Repeat `--repo` for each one. |

Both `--rules` and `--repo` also accept a git URL, and it will clone it
for you:

```bash
eng setup ~/engineering-os \
  --rules git@github.com:truelogics/engineering.git \
  --repo  ~/code/your-application
```

You will see it work through four steps, ending in something like:

```
[2/4] Repositories
Attached engineering (~/code/your-team-rules-repo)
  38 scanned, 38 added, 0 updated, 0 unchanged, 0 errors

[3/4] engineering-mcp
Not installed. Running: go install github.com/truelogics/engineering-mcp/...

[4/4] Claude Code
Installing Engineering OS for Claude Code

  ✔  Workspace
  ✔  Claude Code registration
  ✔  /review-branch command

Done.
```

**`--rules` is the part that matters.** Skip it and everything still
installs, works, and has nothing to say — every review is told "no rule
governs these files", which looks exactly like a correct answer. If you
are not sure which repository holds your rules, point it at wherever your
team's markdown lives and refine it later.

### Step 3 — Check it

```bash
cd ~/code/your-application
eng doctor
```

Eight checks. All `✔` means you are done. If something failed, **fix the
first `✘` and run it again** — the ones below it are usually just knock-on
effects of the same problem. Each failure prints the command that fixes
it.

### Step 4 — Use it

```bash
cd ~/code/your-application
claude
```

Then type:

```
/review-branch
```

**Type that command — do not ask in your own words.** Saying "review my
branch" lets any other review tool on your machine answer instead, and
when that happens none of this is used. Measured on the same commit,
minutes apart: `/review-branch` consulted the team's knowledge 9 times;
"Review my current branch." consulted it 0 times.

---

## Jargon you will see

| Word | What it actually is |
|---|---|
| **workspace** | The folder from step 2. One search index covering the repositories you attached. |
| **rulebook** | The repository you passed to `--rules`. |
| **attach** | Add a repository to the workspace and index it. |
| **index** | The searchable copy of your documents, in `.eng/memory.db`. Rebuild it when documents change. |
| **taxonomy** | A short file saying what a folder contains — "everything in `adr/` is a decision". Optional, but it is what makes documents findable by kind. |

---

## Everyday use

```bash
eng update          # re-index after your documents change — do this, nothing is automatic
eng status          # what is indexed, and whether your rules were found
eng search "authentication"
eng ask "how do we handle permission caching?"
eng doctor          # check everything and say what to fix
```

**Adding another repository later:**

```bash
eng setup ~/engineering-os --repo ~/code/another-application
```

`eng setup` is safe to run again as often as you like.

**Getting more out of your own repository** — by default only documents
with a `doc:` line at the top are understood by kind. To classify the
rest, run this inside a repository and it proposes a mapping and asks
before writing anything:

```bash
eng taxonomy auto
```

Full command reference: [`docs/cli/CLI.md`](docs/cli/CLI.md). Longer
install guide with what to do when things break:
[`engineering-mcp/INSTALL.md`](https://github.com/truelogics/engineering-mcp/blob/main/INSTALL.md).

---

## How it fits together

`eng` is the **shell of the Engineering OS**
([RFC-0008](https://github.com/truelogics/engineering/blob/main/RFC-0008-eng-cli.md)),
not just this repository's CLI. It coordinates and delegates: `eng
doctor` runs `engineering-mcp doctor`, `eng review` hands over to Claude
Code. You should never need to know which repository answers what.

```
You  →  eng            (index, search, setup)
        engineering-mcp (serves the index to Claude Code)
        Claude Code     (does the actual reviewing)
```

This repository is the kernel: store, organize, retrieve and connect
engineering knowledge. Everything else in the OS is built on top of it.

Agents start fresh every session, and knowledge lives in scattered docs
and people's heads. This turns those documents into memory an agent can
load on demand.

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
[`roadmap/NOW.md`](https://github.com/truelogics/roadmap/blob/main/NOW.md) and milestones in
[`roadmap/MILESTONES.md`](https://github.com/truelogics/roadmap/blob/main/MILESTONES.md), so there's exactly one
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
3. Org-wide decisions also land in [`engineering/ADR/`](https://github.com/truelogics/engineering/tree/main/ADR/)

Code dirs: `cmd/`, `internal/` and `pkg/` are implemented (see
[`internal/README.md`](internal/README.md) for the package map);
`tests/` stays reserved — see its README for why.

## Related repos

| Repo | Role |
|------|------|
| [`engineering/`](https://github.com/truelogics/engineering) | Source docs & rules consumed by memory |
| [`roadmap/`](https://github.com/truelogics/roadmap) | Company priorities |
| [`vision/`](https://github.com/truelogics/vision) | Company north star |

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
