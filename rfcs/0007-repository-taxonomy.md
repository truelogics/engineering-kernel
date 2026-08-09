---
doc: RFC
audience: [human, agent]
status: accepted
owner: ai-memory
last_reviewed: 2026-08-09
---

# RFC-0007: Repository Taxonomy

## The evidence

Sprint 12 pointed Engineering OS at its first repository outside this
organization: a 12,500-file monorepo with 416 planning documents, a
72-document engineering handbook and 124 test documents.

Asked to review a branch there, the reviewer produced a blocking finding
that rested entirely on a written decision:

> the block_data backfill was deliberately dropped —
> `plans/to-do/block-fractional-indexing-vorder-v2.md` says the worker
> code was removed and the read sort is "retained here for reference
> only".

That document was in the workspace. It indexed successfully. Full-text
search would have returned it. The reviewer reached it with a `grep` and
a `sed`, and never called Engineering OS at all.

The reason is one row:

```
plans/to-do/block-fractional-indexing-vorder-v2.md | unknown
```

662 of that project's 726 documents classify as `unknown` — 91%, against
36% here. `get_context` can only offer an unclassified document as one
keyword hit among hundreds; it can never return it as *the decision that
governs this change*.

This is not a retrieval failure. Retrieval worked. The kernel held the
document and could not say what it was.

## The diagnosis

`inferDocType` recognizes six families — `RFC`, `ADR`, `RULE`, an
architecture set, a roadmap set, `README` — inferred from front matter
and a few path conventions. Those names are this organization's. Real
repositories organize knowledge as `plans/`, `design/`, `proposals/`,
`decisions/`, `notes/`, `research/`, `spikes/`, `epics/`, `playbooks/`,
`runbooks/`, `handbook/`, `specs/`, `product/`.

The taxonomy is not wrong. It is *ours*, and it is closed. A repository
can follow `doc-front-matter.md` exactly and still be unclassifiable,
which makes this the first onboarding problem rather than a documentation
inconsistency: it is the state of every repository on the day it adopts
Engineering OS.

Two responses were rejected before this one.

**Extend the recognized set.** Adding fifty type names moves the problem
rather than solving it. The next company names things differently, and
the kernel needs a release to learn each vocabulary.

**Guess from paths.** Hardcoding `plans/` → decision bakes one
organization's conventions into a shared kernel, and is wrong the first
time a repository uses `plans/` for sprint chores.

## The design

Two concepts.

### 1. Canonical types — small, closed, ours

The vocabulary the platform reasons over. Deliberately few: a set large
enough to distinguish a rule from a decision from a guide, and small
enough that every repository can map onto it.

| Canonical | Kernel `DocType` | Context section |
|---|---|---|
| `Rule` | `rule` | Rules |
| `Decision` | `adr` | Related ADRs |
| `Architecture` | `standard` | Architecture |
| `Specification` | `specification` *(new)* | Specifications |
| `Guide` | `guide` *(new)* | Guides |
| `Planning` | `roadmap` | Roadmap |
| `Reference` | `readme` | Documentation |
| `Other` | `unknown` | — |

Six of the eight already exist under this organization's names. Only
`Guide` and `Specification` are new constants, so this is additive:
nothing currently classified changes type, and no exported signature
moves (`KERNEL_POLICY` Rule #2).

### 2. Repository mapping — open, per-repository, versioned with the code

The repository teaches the kernel what its own directories mean, in a
file at its root:

```yaml
# .engineering.yaml
taxonomy:
  plans/**:     Decision
  handbook/**:  Guide
  product/**:   Specification
  research/**:  Reference
```

Patterns are the glob syntax RFC-0005 already defines for `applies_to`,
matched by the same code — one path-matching semantics in the kernel, not
two.

The file lives *in the repository*, not in the workspace, because the
answer belongs to the project and should travel with it, be reviewed with
it, and change with it.

### Precedence: taxonomy fills silence, it never overrules

A path mapping applies **only to documents the kernel would otherwise
classify `unknown`.**

This is the whole safety argument. An explicit `doc: RULE` still wins.
Built-in path conventions still win. Every document classified today
keeps its type, so no existing consumer sees a reclassification — the
mapping can only turn `Other` into something more specific.

It also matches how the two kinds of statement differ in authority. Front
matter is what a document says about itself; a taxonomy is what the
repository says about a directory. The specific claim wins over the
general one.

### Where it is applied

In the Indexer, after Parse, not inside the Parser. `kernel.Parser` takes
a `RawDocument` and knows nothing of repositories; threading a
repository-level fact through it would widen an interface to carry
something only one implementation could use. The Indexer already holds
the `Repository`, loads the taxonomy once per run, and applies it to
documents that came back `unknown`.

## Acceptance

The RFC is satisfied when, on the outside project that motivated it:

1. `plans/to-do/block-fractional-indexing-vorder-v2.md` classifies as a
   decision rather than `unknown`;
2. `get_context` for a change to the block ordering code returns it under
   Related ADRs, not as an incidental keyword hit;
3. this organization's own six repositories classify **identically** to
   before, document for document.

The third is the one that matters. A taxonomy change that quietly
reclassifies existing documents would be indistinguishable from a
regression in every consumer that groups by type.

## Out of scope

- Inferring a taxonomy from repository structure. A guess presented as
  the repository's own statement is worse than no statement.
- Content-based classification. Reading a document to decide what it is
  is a different capability with different evidence behind it, and none
  has been gathered.
- Per-document overrides beyond front matter, which already exists.
