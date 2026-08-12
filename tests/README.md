---
doc: tests
audience: [human, agent]
status: reserved
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# tests/

Integration and end-to-end tests — ingest fixtures, query assertions, regression
suites for rule matching.

> **Still reserved, by choice, not by omission.** Sprint 2's integration and
> end-to-end coverage (e.g. `internal/indexer`'s
> `TestIndexEndToEndWithRealComponents`, `internal/cli`'s
> `TestDefinitionOfDone`) lives as `_test.go` files next to the package
> under test, per normal Go convention — not here. This folder stays
> reserved for fixture-heavy or cross-package scenarios that don't have one
> obvious package to live next to, if that ever comes up.
