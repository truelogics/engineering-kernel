---
rfc: 0006
title: "Evidence Capability"
status: draft
author: founding-team
created: 2026-08-09
supersedes:
superseded_by:
resulting_adr:
---

# RFC 0006 — Evidence Capability

## Summary

Move evidence verification, excerpt normalization and confidence scoring
out of Engineering Review's `Validator` and into `pkg/memory`, where two
consumers can reach it. Nothing else moves: rendering, review objects
and the decision of *what* to claim evidence for stay with the consumer.

## Motivation

Evidence verification is real, works, and is covered by tests. It lives
in `engineering-review/internal/validator` and nowhere else, which
`engineering/CAPABILITIES.md` records as **Consumer-only** — implemented
inside one application, callable by nothing.

Sprint 8 turned that classification into a concrete blocker. Building
`engineering-mcp` meant choosing between duplicating `Validator` — which
would leave two implementations of an anti-hallucination check to drift
apart, and which that repository is explicitly forbidden from doing —
and shipping without the capability. It shipped without.

So two independent consumers now need the same logic, which is the bar
Rule #1 sets for a kernel change:

- **Engineering Review** verifies that a model's cited excerpt really appears in
  the document it claims, and drops the claim when it doesn't.
- **engineering-mcp** wants to offer the same check to any MCP client,
  so a model writing "per ADR-0003, ..." can have that quote verified
  before a human reads it.

Neither is speculative. The first ships today; the second is a
documented rejection in `engineering-mcp/README.md` waiting on this RFC.

## What is evidence

**Evidence is a claim that a specific document says a specific thing,
verified against what retrieval actually returned.**

Three parts, all required:

- **Document** — a path that retrieval returned, qualified by the
  repository it came from. A workspace holds several repositories, and
  `README.md` names a different document in each.
- **Excerpt** — text asserted to appear in that document.
- **Confidence** — how the excerpt was matched, not how sure anyone
  feels.

What makes it evidence rather than a citation is that the kernel checked
it. An unverified quote is an assertion; only a verified one is
checkable by the person reading it.

### Valid evidence

An excerpt is valid when it is genuinely present in the content
retrieval returned for that document. Two directions count, and they
mean different things:

| Confidence | Condition | Reading |
|---|---|---|
| `high` | The excerpt appears inside the retrieved content | The quote is exact |
| `medium` | The retrieved content appears inside a longer excerpt | The quote is real but extends past what was retrieved |

Anything else is **not evidence** and is reported as such. There is
deliberately no `low`: "evidence we could not verify" is not evidence,
and offering it as a weak grade invites a consumer to show it anyway.
`Confidence` keeps a `low` constant for consumers that grade their own
non-evidence claims; the kernel never returns it.

Matching normalizes case, whitespace, and the FTS5 highlight markup
(`**term**`, `...`) that snippets carry for a terminal's benefit — so a
model quoting text it was shown verbatim is not failed for reproducing
the markup, or for rewrapping a line.

### Why verification is bounded, and honestly so

The kernel can only verify an excerpt against what it returned, and what
it returns is a search highlight of roughly 40–200 characters, not the
document (`engineering-review/KERNEL_REQUIREMENTS.md` #15). So this capability
answers "does this quote match the passage retrieval surfaced" and not
"does this quote appear anywhere in this document." A true quote from
elsewhere in the same file fails.

That is a real limitation and it is the right one to ship with: the
alternative — reading files from disk to verify against — makes evidence
depend on the working tree rather than on the index, and would report
success for a document the consumer was never actually shown. #15
remains the fix.

## Proposal

### API

```go
// Evidence is a verified claim that a document says something.
type Evidence struct {
    Document   string
    Repository string
    Excerpt    string
    Confidence Confidence
}

type Confidence string

const (
    ConfidenceHigh   Confidence = "high"
    ConfidenceMedium Confidence = "medium"
    ConfidenceLow    Confidence = "low"
)

// VerifyEvidence checks that excerpt genuinely appears in the content
// this package returned for document. Reports false when the document
// isn't in the package, or the excerpt isn't in its content.
func (p ContextPackage) VerifyEvidence(document, excerpt string) (Evidence, bool)
```

A method on `ContextPackage` rather than on `Memory`: verification is
pure, given what retrieval returned, and needs no open workspace. That
also makes the boundary self-evident — a consumer can only verify
against context it actually received, which is the property the whole
capability exists to guarantee.

`document` accepts either a bare path (`rules/logging.md`) or a
repository-qualified one (`engineering:rules/logging.md`). A bare path
that is ambiguous across repositories fails verification rather than
guessing, since guessing would attribute a quote to a document nobody
chose.

### Division of responsibility

| Concern | Owner |
|---|---|
| What counts as valid evidence | **Engineering Kernel** |
| Verifying an excerpt against retrieved content | **Engineering Kernel** |
| Confidence calculation | **Engineering Kernel** |
| Excerpt normalization and markup cleaning | **Engineering Kernel** |
| Deciding what to claim evidence *for* | Consumer |
| What to do when verification fails | Consumer |
| Rendering evidence for a human | Consumer |
| Review objects, findings, severity | Consumer |

Engineering Review keeps dropping unverifiable claims silently, because a review
is a finished artifact. `engineering-mcp` will report the failure to the
client instead, because a model can revise. Same kernel answer, opposite
consumer policy — which is the test that the split is in the right
place.

## Alternatives considered

**Leave it in Engineering Review and let `engineering-mcp` import it.** Rejected
by Rule #3: applications never import another application's `internal/`,
and promoting `engineering-review/internal/validator` to `engineering-review/pkg/` would
make Engineering Review a kernel — a second one, for a capability that belongs in
the first.

**Verify against files on disk instead of retrieved snippets.** Rejected
above: it makes evidence depend on the working tree rather than the
index, and would verify quotes from text the consumer was never shown.

**Return `low` confidence for unverified excerpts.** Rejected. Grading
non-evidence invites showing it, and "unverified evidence" is a
contradiction a reader cannot act on.

## Trade-offs & risks

- **A `pkg/memory` addition is permanent.** `Evidence`, `Confidence` and
  `VerifyEvidence` are public surface from the moment they land (Rule
  #2). All three are additions, so nothing breaks.
- **Verification stays snippet-bounded** until #15 is fixed, and will
  reject true quotes from elsewhere in a document. Documented on the
  method, not hidden.
- **Two consumers, two policies on failure.** If a third wants a third
  policy, the kernel is still only being asked "is this true?", which is
  the signal the boundary is right.

## Rollout

1. `Evidence`, `Confidence`, `VerifyEvidence` in `pkg/memory`, with the
   test cases lifted from Engineering Review's `Validator`.
2. Engineering Review's `Validator` calls it; its own matching, normalization and
   cleaning are **deleted**, not wrapped. A promotion that leaves the
   original in place has created the duplication it was meant to prevent.
3. `engineering-mcp` exposes `verify_evidence`.

Step 2 is the acceptance criterion: Engineering Review's behavior must be
unchanged, its tests must pass untouched, and the duplicate logic must
be gone.

## Open questions

- Should evidence carry a **section/heading**? `KERNEL_REQUIREMENTS.md`
  #14 asks for it and nothing exposes it yet, so `Evidence` has no
  `Section` field rather than an always-empty one.
- Should the kernel *find* supporting evidence for a claim, not only
  verify a proposed one? That is a retrieval question ("what supports
  X?") rather than a verification one, no consumer has asked for it, and
  Rule #5 says it waits until one does.
