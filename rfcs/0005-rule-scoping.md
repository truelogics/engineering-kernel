---
rfc: 0005
title: "Rule Scoping: applies_to"
status: draft
author: founding-team
created: 2026-08-09
supersedes:
superseded_by:
resulting_adr:
---

# RFC 0005 — Rule Scoping: `applies_to`

## Summary

Engineering rules are currently selected by text similarity alone. Every
rule already declares which files it governs, in an `applies_to` field in
its front matter, and retrieval reads none of it. This RFC makes
`applies_to` load-bearing: a consumer that knows which files a task
touches can ask for the rules that actually govern those files.

Scoped to that. Named workspaces, repository priority, rules-only
repositories and cross-repository duplicate-path resolution are all real
and all deferred — they are optimizations of a capability that works,
whereas rule selection is a capability that demonstrably returns the
wrong answer.

## Motivation

The evidence is one line from Sprint 7's first successful Category B
run. Reviewing a change touching only Go files in `ai-review`, the
retrieved rule was:

```
Rules: 1
  0.437  engineering    rules/ts-no-floating-promises.md
```

A TypeScript rule, for a Go diff. It was returned because the review's
task text mentioned errors and promises were mentioned in the rule, and
nothing anywhere compared the rule's declared scope against the files
under review. The rule's own front matter says exactly what it governs:

```yaml
applies_to: "**/*.ts, **/*.tsx"
```

This is `ai-review/KERNEL_REQUIREMENTS.md` #6, which sat speculative for
four sprints and became measured the first time the surrounding machinery
worked well enough to expose it.

At ten rules this is noise a reader can skip. The failure mode is that it
does not stay noise: a rulebook that grows to fifty rules across four
stacks returns a rules section that is mostly irrelevant, and a reviewer
who learns to skim the rules section is a reviewer for whom grounding has
stopped working. The cost of getting this wrong is not a bad finding; it
is the section quietly becoming ignorable.

**Why this is a kernel capability and not an AI Review feature.**
"Which rules govern these files?" is asked by every consumer that will
ever exist — a review, an IDE showing rules for the open file, CI
checking a changed path, an agent about to edit something. The matching
logic, the shorthand, and the fallback behavior are identical for all of
them. If AI Review implemented it, it would own the one piece of the
answer that nothing else could reuse (`engineering/CAPABILITIES.md`'s
four-question test, and `KERNEL_POLICY.md` Rule #4).

## Proposal

### The `applies_to` field

A rule declares its scope in front matter as a comma-separated list of
patterns:

```yaml
---
doc: RULE
id: go-wrap-errors
severity: error
applies_to: "go"
---
```

Three pattern forms, in increasing specificity:

| Form | Example | Matches |
|---|---|---|
| Bare token | `go` | any file with that extension — expands to `**/*.go` |
| Glob | `**/*.ts, **/*.tsx` | paths matching the glob |
| Directory glob | `internal/store/**` | paths under a directory |

The bare-token shorthand exists because it is what rule authors actually
write. `applies_to: go` is the common case by a wide margin, and
requiring `**/*.go` for it would mean most rules carry a pattern whose
syntax matters more than its content. A token containing no `/`, `*` or
`.` is treated as an extension.

An absent or empty `applies_to` means the rule is universal and applies
to every path. This is the correct default: a rule whose author did not
think about scope is more likely to be a general engineering standard
(`pr-single-purpose`) than a stack-specific one, and the failure mode of
the alternative — silently dropping unscoped rules — is far worse than
showing one rule too many.

### Glob semantics

A small matcher, because Go's `path.Match` does not support `**`:

- `*` matches any run of characters except `/`
- `**` matches any run of characters including `/`
- `?` matches exactly one character except `/`
- matching is case-sensitive and operates on forward-slash paths

A pattern with no `/` and no `**` is matched against the path's base name
as well as the whole path, so `*.go` behaves the way an author expects
against `internal/store/user.go`.

### Retrieval: scope selects rules, text only orders them

**Amended after implementation.** This RFC originally specified scoping
as a *filter* over search results, and step 5 of the rollout said that if
the acceptance criterion did not hold, the RFC was wrong. It did not
hold, so here is the correction.

Filtering removed the TypeScript rule and surfaced nothing in its place —
the Rules group went from one wrong rule to zero rules. The reason is
visible in the data. Reviewing a change to `ai-review`'s Claude provider
and Markdown formatter, the task's vocabulary was:

```
category reported security severity decoder default float64 renders ...
```

while the rule that actually governs that change reads:

> A silent fallback produces output that looks exactly like the real thing.

They share no words. No text search would ever have connected them, at
any ranking, with any query construction.

**A violation rarely quotes the rule it violates.** That is the general
form, and it means text similarity is the wrong *selector* for rules
specifically — a rule is relevant because of what it governs, not
because of what it says. Rules are therefore selected by declared scope
and merely *ordered* by text relevance: a rule the search also matched
keeps its score and sorts first, so a rule the change genuinely talks
about still outranks one that merely governs the same file type.

This applies to rules alone. Every other Knowledge Type is still
selected by search, because an ADR or an architecture document is
relevant for exactly the reason search is good at finding: it discusses
the thing being changed.

Selecting by scope requires listing rules independently of the search's
result budget, so `kernel.Storage` gains `ListDocumentsByType`. The
returned set is capped at ten so a large rulebook cannot crowd out every
other section.

### Retrieval options

`kernel.Retriever` gains options carrying the paths a task concerns:

```go
type RetrieveOptions struct {
    // ChangedPaths are the files this task concerns, repository-relative.
    // Empty means "no path information" — scoping is skipped entirely.
    ChangedPaths []string
}

Retrieve(ctx context.Context, task string, opts RetrieveOptions) (RetrievalBundle, error)
```

Scoping applies **only to the Rules group**. Architecture documents,
ADRs and RFCs carry no `applies_to` and are not filtered — an ADR about
global mutable state is relevant to a change whether or not the ADR
names the files involved.

Filtering is a filter, not a re-rank: a rule that survives keeps the
score search gave it. Ranking scoped rules by pattern specificity (a rule
matching `internal/store/**` outranking one matching `go`) is a plausible
improvement with no evidence behind it yet, and Rule #5 says that means
it waits.

### Fallback behavior

The three cases that matter, and why each resolves the way it does:

1. **No paths supplied** — no filtering. `eng ask "how does indexing
   work?"` has no changed paths and must keep working exactly as it does
   today. This is also what makes the change backward compatible.
2. **Paths supplied, rule has no `applies_to`** — the rule is kept. See
   above: unscoped means universal.
3. **Paths supplied, no rule matches any of them** — the Rules group
   comes back empty. An empty rules section is an honest answer: it says
   this organization has not written a rule governing these files. The
   alternative, falling back to unfiltered results, would reintroduce the
   exact failure this RFC exists to fix, at precisely the moment it is
   least justified.

### Public SDK

`Memory.Context` keeps its signature. A new method takes options:

```go
type ContextOptions struct {
    ChangedPaths []string
}

func (m *Memory) ContextFor(ctx context.Context, task string, opts ContextOptions) (ContextPackage, error)
```

Adding a method is not a breaking change (`KERNEL_POLICY.md` Rule #2,
and `engineering/rules/rfc-before-public-api-change.md`), so every
existing consumer keeps compiling and behaving identically. `Context`
becomes `ContextFor` with zero options.

## Alternatives considered

**Infer scope from the rule's text.** Asking a model, or keyword-matching
"Go" in the body, would need no front-matter contract. Rejected: it makes
rule selection non-deterministic and unexplainable, and the declaration
already exists. A rule that says what it governs should be believed.

**Filter in the consumer.** AI Review has the changed paths and could
drop rules itself. Rejected on the four-question test: every consumer
needs the same logic, and the one that implemented it would own the piece
none of the others could reuse.

**Scope every document type, not just rules.** Architecture documents
could declare `applies_to` too. Rejected as speculative — no evidence
that architecture retrieval suffers from the same problem, and #16 (group
labels collapsing) is the measured architecture-retrieval defect, not
scoping.

**Make unscoped rules non-universal.** Requiring `applies_to` on every
rule would be stricter and arguably cleaner. Rejected: it turns an
authoring omission into silent invisibility, which is the same class of
failure as the YAML rulebook that nothing could index.

## Trade-offs & risks

- **A mis-declared `applies_to` now hides a rule.** Previously the field
  was decorative, so an error in it was harmless; now it silently removes
  a rule from every review of the files it should have governed. This is
  a real regression in failure mode, accepted because the alternative is
  the current behavior, and mitigated by the universal-by-default rule
  for absent values.
- **Extension shorthand is ambiguous for extensionless files.**
  `applies_to: Dockerfile` reads like a filename but is treated as an
  extension. Documented; not solved.
- **Paths are repository-relative, and a workspace holds several
  repositories.** A rule in `engineering/` is matched against paths from
  the repository under review, which is the intent, but means a pattern
  like `internal/**` matches any repository's `internal/`. Acceptable
  while rules are organization-wide; revisit if per-repository rules
  appear.

## Rollout

1. `applies_to` parsing and the glob matcher, with tests — no behavior
   change until something calls it.
2. `RetrieveOptions.ChangedPaths` through `kernel.Retriever` and the
   retriever implementation; `Retrieve(ctx, task)` behavior preserved for
   empty options.
3. `Memory.ContextFor` on the public SDK.
4. AI Review passes `diff.Paths()`.
5. Re-run the Category B measurement: the same review that returned a
   TypeScript rule for a Go diff should return the Go rules that govern
   it, and no TypeScript ones.

Step 5 is the acceptance criterion. If it does not hold, this RFC is
wrong and gets amended rather than shipped.

### Result

It did not hold on the first implementation, and the RFC was amended —
see "scope selects rules, text only orders them" above. After the
amendment, the same review that motivated this RFC returns:

| Change under review | Rules returned |
|---|---|
| `markdown.go`, `claude.go` (Go) | `deterministic-stages`, `go-wrap-errors`, `no-internal-imports`, `no-silent-fallback`, `rfc-before-public-api-change`, plus the two universal rules |
| `docs/PIPELINE.md` (markdown) | `doc-front-matter`, plus the two universal rules |

No TypeScript rule appears in either. No Go rule appears for the
markdown change. `no-silent-fallback` — the rule that governs the
provider change, and the one no text search could reach — appears for
the Go change.

One further defect surfaced while measuring: `rules/README.md`, the
rules index, was returned as a rule in every review. It carries no
`applies_to`, so it is universal, and it is not a rule at all. A page
that declares itself an index (`doc: rules-index`) now classifies as
documentation — an index describes a directory rather than being an
instance of what the directory holds.

## Open questions

- Should pattern specificity affect ranking? Deferred pending evidence
  that an over-broad rule crowds out a precise one in practice.
- Should `severity` in front matter influence retrieval order — an
  `error` rule ranked above a `warn` one? Plausible, unmeasured, and
  cleanly separable from scoping.
- Extensionless well-known filenames (`Dockerfile`, `Makefile`) need a
  form the shorthand does not currently express.
