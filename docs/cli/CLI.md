---
doc: CLI
audience: [human, agent]
status: living
owner: engineering-kernel
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

### `eng setup [path] --rules <repo> [--repo <repo>]`

Everything between a machine with `eng` on it and a machine where
`/review-branch` cites your organization's own decisions:

```bash
eng setup ~/engineering-os \
  --rules git@github.com:truelogics/engineering.git \
  --repo  ~/code/your-application
```

Four steps, reported as it goes: create the workspace; clone, attach and
index each named repository; install `engineering-mcp` if it is missing;
hand over to `engineering-mcp install`, which registers the server with
Claude Code and installs the `/review-branch` command.

`--rules` and `--repo` each take a local path or a git URL, and may be
repeated. They differ only in that setup warns when no `--rules` was
given: a workspace with no rulebook answers every question about rules
with a confident "none", and that is indistinguishable from a correct
answer.

**Re-runnable.** Run it again to add a repository, or after rebuilding,
to repoint Claude Code at the new binary. One unreachable path is
reported and the others still attach.

This is orchestration and nothing else. What it contributes over typing
the six commands yourself is the order, and one decision people got
wrong by hand — see `eng init` below.

### `eng init [path]`

Creates `.eng/memory.db` and registers the directory as a repository.
`eng workspace create` is the same command under the workspace
vocabulary. `eng setup` calls it for you.

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

`eng setup` makes that decision from the filesystem — a workspace root
that is not itself a git repository is a container, and is detached —
because the manual step was one whose reason nobody could see from where
it was written, and it was skipped accordingly.

### `eng taxonomy`

Shows what this repository has declared its directories mean
(`.engineering.yaml`, RFC-0007), or explains how to say so when it has
not. It never writes anything.

### `eng taxonomy auto`

Proposes mappings from what the repository already contains, shows what
they would do, and writes the file only if you say yes.

**It proposes; you approve; the file is yours.** Nothing is written
without an explicit `y`, and a closed stdin counts as no — a command that
read EOF as approval would write the file in exactly the automated
contexts where nobody is watching.

Signals are deterministic and local: unambiguous directory names, and the
`doc:` front matter of documents that already declare themselves. No
model, no network, so the same repository state always yields the same
file and a proposal can be reviewed in a diff.

It is conservative on purpose. A directory called `docs/` or `notes/`
gets no mapping unless its own documents say what it holds, because a
wrong mapping returns confidently mislabelled documents, which is worse
than leaving them unknown. Everything considered and passed over is
listed, with the reason.

The impact figures are measured, not estimated: the proposal is rendered
to YAML, parsed by the same parser indexing uses, and applied with the
same matcher. What it says will happen is what happens.

```bash
eng taxonomy auto              # propose, show, ask
eng taxonomy auto --update     # propose changes to an existing file, and still ask
eng taxonomy auto --yes        # approve on the command line instead of at the prompt
```

An existing `.engineering.yaml` is never touched without `--update`, and
`--update` **merges rather than replaces**: it adds what it has evidence
for and never rewrites or removes a line you wrote. A proposal has
evidence for what it can see and none for what it cannot, and treating
"no evidence" as "delete this" would discard a decision you made on
purpose. Under `--update` the impact baseline is what your existing file
already achieves, so the numbers show the delta rather than crediting the
proposal for your work.

A file that does not parse is never replaced at all — it is a statement
the repository was trying to make, and overwriting it would destroy the
evidence of the mistake along with it.

Front-matter precedence is unchanged (RFC-0007): a document's own `doc:`
always wins over any mapping, and documents where the two disagree are
reported rather than quietly ignored.

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

Most of what it reports as broken is fixed by `engineering-mcp install`,
which `eng setup` runs — the checks name it where that is the answer.

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

RFC-0007 originally placed inference out of scope. `engineering-kernel` RFC-0009
amends that: the boundary is writing, not inferring, and an inference
nobody has approved is a draft rather than the repository's statement.

### On proposing rather than deciding

`VALIDATION_PHASE_1.md` says mappings are never invented for a repository
by someone who does not work in it. `auto` respects that by never being
the one who decides: it assembles evidence, shows its reasoning, and
stops. The approval is the decision, and it belongs to whoever owns the
repository.

That is also why a low-confidence directory is omitted rather than
guessed at. A proposal you have to correct is worse than one that admits
it does not know, because the correction only happens if you notice.

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
