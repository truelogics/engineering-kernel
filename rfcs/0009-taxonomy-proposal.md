---
doc: RFC
audience: [human, agent]
status: accepted
owner: ai-memory
last_reviewed: 2026-08-12
---

# RFC-0009 — Taxonomy proposal

Amends [RFC-0007](0007-repository-taxonomy.md).

## Why this exists

RFC-0007's "Out of scope" says:

> Inferring a taxonomy from repository structure. A guess presented as
> the repository's own statement is worse than no statement.

`eng taxonomy auto` infers a taxonomy from repository structure. This RFC
is the amendment, because a scope boundary in an accepted RFC should be
moved in a document rather than crossed in a commit message.

## Which review exposed the need

`engineering-mcp:docs/reports/DOGFOOD_LOG.md`, "The onboarding flow this
changes" — the Sprint 12 run against Pivot, a repository outside this
organization, where 91% of documents were unclassified and a review's
blocking finding rested on a planning document retrieval could only ever
return as one keyword hit among 662 siblings.

RFC-0007 fixed the mechanism and, in doing so, added a step:

```
eng init  →  teach the repository to describe itself  →  eng index
```

> That middle step is the first genuine onboarding problem the project has
> found, and it was invisible until Engineering OS was pointed at
> something it had not been built alongside.

So the limitation is named, reproduced, and recorded — the same entry that
justifies this work also contains the sharpest objection to it, that a
stranger guessing directory meanings *"is worse than the repository's own
team writing four lines."* Both are true. The resolution is below.

## What changes, and what does not

RFC-0007's sentence rules out **a guess presented as the repository's own
statement**. The object of that sentence is the presentation, not the
inference. An inference nobody has approved is not a statement; it is a
draft.

Amended, the boundary reads:

> Inferring a taxonomy from repository structure **and writing it**. A
> guess presented as the repository's own statement is worse than no
> statement. Inference offered as a proposal, which a person must approve
> before it becomes the repository's statement, is permitted — see
> RFC-0009.

Unchanged, and load-bearing:

- **Front matter still wins.** A document's `doc:` outranks every mapping,
  proposed or hand-written. Documents where the two disagree are reported.
- **The file is still the repository's.** `.engineering.yaml` lives in the
  repository, is reviewed with it, and is owned by the team that works in
  it. Nothing writes it without an explicit yes.
- **Content-based classification is still out of scope.** The signals are
  paths and existing front matter. Reading a document to decide what it is
  remains a different capability with no evidence behind it.

## The rule that makes a proposal safe

`engineering:VALIDATION_PHASE_1.md` says mappings are never invented for a
repository by someone who does not work in it, and adds: *"A mapping
written by the team that owns the repository is the design; anything else
is a demonstration."*

That standard is met by never being the one who decides. The proposer
assembles evidence, shows its reasoning, states its measured effect, and
stops. The approval is the design decision, and it belongs to whoever owns
the repository. Concretely:

1. **It cannot write without a yes.** EOF counts as no, so an unattended
   run declines rather than writes.
2. **It never removes or rewrites a line a person wrote.** `--update`
   merges. A proposal has evidence for what it can see and none for what
   it cannot, and treating "no evidence" as "delete this" would discard a
   decision made on purpose.
3. **It declines rather than guesses.** A directory whose name is
   ambiguous and whose documents declare nothing gets no mapping, and is
   listed as considered and passed over. The objective is useful
   proposals, not coverage.
4. **Its claims are measured, not estimated.** The proposal is rendered to
   YAML, parsed by `domain.ParseTaxonomy`, and applied with
   `Taxonomy.MappingFor` — the code that runs at index time. What the
   prompt says will happen is what happens.
5. **It is deterministic and local.** No provider, no model, no network.
   The same repository state renders the same file byte for byte, so a
   proposal can be reviewed in a diff instead of taken on trust.

Point 3 is the one that carries the argument. The failure mode RFC-0007
names is a confidently mislabelled document, and the way a proposal
produces one is by covering a directory it does not understand. A
proposer that declines is not a weaker version of one that guesses; it is
the only kind that can be offered to someone who will not check every
line.

## Where it lives

`internal/taxonomy`, not the CLI. It answers the same question RFC-0007
answers by hand, and it has to agree exactly with what indexing does —
both of which make it kernel work. The CLI reads the repository, renders,
asks, and writes.

Signals, in full:

- **Directory names**, from a closed table of unambiguous ones. `docs`,
  `design`, `notes` and `misc` are deliberately absent: they hold whatever
  a project puts in them. Evidence for a directory comes only from its
  direct children, so an ambiguous parent cannot inherit a specific
  child's meaning and then claim its siblings.
- **Existing front matter**, needing both a two-thirds majority and a
  floor of two documents. One typed document among twenty is a
  coincidence, and a proposal resting on a coincidence is the
  mislabelling this exists to avoid.
- **Neither, when they disagree.** A name and a majority that contradict
  each other are reported as a conflict, not resolved by preferring one.

A directory is proposed for only if it would classify a document that is
currently unknown. That also keeps the proposer from duplicating the
markdown parser's own path rules — `rules/`, `adr/`, `rfc/` and
`roadmap/` are already classified, so they fall out with no special case.

## Acceptance

1. A repository with no taxonomy gets a proposal that is useful and
   partial, with every omission explained. ✅ Verified on `engineering`
   (nothing proposed; `prompts/`, `skills/`, `templates/` and 12 root
   documents each accounted for) and on a synthetic messy layout
   (three mappings, `weird-folder/` and `docs/` declined).
2. The predicted impact equals the observed impact after `eng index`. ✅
   Verified: 6 unknown → 1 predicted, 1 observed.
3. No file is written, replaced, or altered without explicit approval. ✅
   Covered by tests including the `--yes --update` path.

## Alternatives considered

**Leave it out.** RFC-0007's position, and defensible: the team that owns
a repository writes better mappings than any inference. It stands as the
recommendation. What it does not survive is the onboarding cost measured
in Sprint 12 — a first-day developer who does not yet know the canonical
vocabulary writes nothing at all, and nothing is worse than a draft they
correct.

**An AI-assisted proposer.** Rejected for now, and not on principle: a
model would read the documents and propose better mappings for exactly
the ambiguous directories this declines to touch. It is rejected because
it would make onboarding non-deterministic, non-local, and non-free, and
because no usage has yet shown deterministic proposals to be
insufficient. Reconsider when a real repository produces a proposal a
person had to substantially rewrite.
