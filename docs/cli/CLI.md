---
doc: CLI
audience: [human, agent]
status: living
owner: ai-memory
last_reviewed: 2026-08-10
---

# eng — the Engineering OS shell

`eng` is the entry point to the whole Engineering OS, not the CLI of this
repository (`engineering/RFC-0008-eng-cli.md`). It coordinates and
delegates; where a capability lives somewhere else, it runs that thing
rather than reimplementing it.

| Layer | Owner |
|---|---|
| kernel — index, retrieve, classify | this repository |
| transport — MCP, Claude Code | `engineering-mcp` |
| reasoning — the review itself | the client |
| **coordination** | **`eng`** |

The point of the arrangement is that a developer never has to know which
row answers their question.

This document supersedes an earlier draft that described a designed
surface of seven commands including `add` and an index-integrity
`doctor`. Neither exists: `add` became `workspace attach`, and `doctor`
became the machine-wide check that `engineering-mcp` implements and `eng`
delegates to. What follows is what the binary does.

## Getting started

### `eng init [path]`

Creates `.eng/memory.db` and registers the directory as a repository.
`eng workspace create` is the same command under the workspace
vocabulary.

When the workspace root is a *parent* of several repositories — the
layout that lets a review see both your code and your rulebook — detach
it immediately afterwards, because a container is not a repository:

```bash
eng init .
eng workspace detach .
eng workspace attach ./your-app
eng workspace attach ./your-rules-repo
```

Left attached, every child repository's documents are indexed a second
time under the root's name, and `repository:path` citations stop
distinguishing them.

### `eng taxonomy`

Shows what this repository has declared its directories mean
(`.engineering.yaml`, RFC-0007), or explains how to say so when it has
not. Nothing is written for you — see `taxonomy suggest` below.

### `eng index [path]`

Runs the full pipeline for **every repository attached to the
workspace**, not just the path given. The workspace is the indexing
boundary, so it is also the re-indexing boundary.

### `eng doctor`

Runs `engineering-mcp doctor`, which checks the binaries, the workspace,
the index, the retrieval, the Claude Code registration, the MCP handshake
and the `/review-branch` command, and names the first thing that is
wrong.

Delegation rather than a second implementation: two sets of checks would
drift, and then disagree about a machine neither could fix. When
`engineering-mcp` is not installed, `eng` says so and reports the
workspace facts it can establish alone.

### `eng review`

Does not review anything. Checks that there is a repository, a workspace,
and an index containing this repository, then hands over to Claude Code
with the exact words to use.

Type `/review-branch` rather than describing the task. Measured on this
organization's own repository, same commit, minutes apart: asked as
"Review my current branch.", another installed review skill claimed the
request and Engineering OS was called zero times in 37 tool calls; asked
as `/review-branch`, nine times in 59. See
`engineering-mcp/docs/reports/TOOL_DISCOVERY_EXPERIMENT.md`.

`--no-launch` prints the instructions without starting Claude Code.

## Everyday

### `eng update [path]` (alias `eng sync`)

Incremental re-index using git as the source of truth for what changed,
including removing rows for deleted files. Covers every attached
repository, like `index`.

### `eng status [path]`

Which workspace answered, whether this repository declares a taxonomy,
how many rules are indexed, how many documents remain unclassified, then
a row per repository.

The rule count is the load-bearing line. A workspace holding no rules
answers every question about them with a confident "none", which reads
exactly like a correct answer.

### `eng search <query>`

Ranked full-text search across the workspace. Every result is qualified
as `repository:path`, because a path is unique only within a repository.

### `eng ask <question>` / `eng context --task "<description>"`

Assembles the engineering context for a question or a task: rules, ADRs,
and related documents, grouped by what they are.

## Workspace

A workspace is one `.eng/memory.db` over one or more repositories,
searched together.

| Command | |
|---|---|
| `workspace create [path]` | create a workspace and register its own directory |
| `workspace attach <path>` | attach a repository and index it |
| `workspace detach <path>` | remove a repository and its documents |
| `workspace list` | every attached repository, with document counts |
| `workspace status` | documents, rules, and what this workspace can answer |

`attach` and `detach` act on the workspace in the **current directory**.
They cannot be told which workspace to use and will not create one, so
run them from the workspace root.

## Meaning

### `eng taxonomy validate`

Parses `.engineering.yaml` and, when the repository is indexed, reports
what it actually achieves: how many documents are already classified, how
many the file would rescue on the next index, and how many would remain
unknown.

Parsing alone is a weak check. A taxonomy can be perfectly well-formed
and match nothing, which is indistinguishable from not having written
one.

### `eng taxonomy suggest`

Deliberately not implemented. A taxonomy is a claim about what your
directories mean, and an inference from directory names presented as a
suggestion is how a wrong mapping gets accepted without anyone deciding
it. `engineering/VALIDATION_PHASE_1.md` is explicit that mappings are
never invented for a repository by someone who does not work in it, and
that includes this tool.

## Machine

### `eng config [path]`

The effective configuration for this directory: the workspace that
resolves here, the index path, whether a taxonomy is present, and where
`engineering-mcp`, `claude` and `git` are found.

`ENGINEERING_WORKSPACE` is listed and labelled: it is read by
`engineering-mcp`, never by `eng`, which resolves the workspace from the
directory it runs in.

### `eng clean [path] --yes`

Deletes the generated `.eng/` directory. Refuses without `--yes`, and
names the repositories that are attached first — the index rebuilds from
your documents, but the record of *which repositories were attached* does
not exist anywhere else.

### `eng version`

## Conventions

- Every command takes an optional path, defaulting to the current
  directory, except `search`, `ask` and `doctor`, which act on the
  workspace resolved from where you are.
- Failures name the file and the stage. An index that reports `1 errors`
  and nothing else has already cost this organization twice.
- An empty answer is stated, not implied. "No rule governs these files"
  and "I did not look" are different, and silence cannot distinguish
  them.
