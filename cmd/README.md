---
doc: cmd
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# cmd/

CLI entrypoints for Engineering Kernel.

> **Kernel MVP implemented.** `cmd/eng` wires `init`, `index`, `search`, and
> `status` to real components (see [`internal/cli`](../internal/cli/)) —
> the four [`CLI.md`](../docs/cli/CLI.md) commands Step 7's Definition of
> Done requires. `add`, `ask`, and `doctor` are designed but not coded yet.
