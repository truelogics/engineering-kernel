---
doc: sdk
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# sdk/

Docs for Engineering Kernel's client libraries — so other Go programs can query
Engineering Memory without shelling out to `eng`.

- [GO_SDK.md](GO_SDK.md) — `pkg/memory`, the only supported way to import
  Engineering Kernel from outside this module. See RFC-0004.

Other languages: not yet — no reason to build one until a non-Go consumer
exists.
