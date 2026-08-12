---
doc: docs-index
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# Docs

Technical documentation for Engineering Kernel, one subfolder per concern. Design
proposals live in [`../rfcs/`](../rfcs/), not here — this folder is where an
accepted RFC's detail gets fleshed out and kept current.

## Index

| Folder | Purpose | Status |
|--------|---------|--------|
| [architecture/](architecture/) | KNOWLEDGE_MODEL.md (what Engineering Knowledge is), ARCHITECTURE.md, DOMAIN_MODEL.md, DATABASE.md, INTERFACES.md, GRAPH.md — the kernel design | living |
| [cli/](cli/) | CLI.md — command reference | living |
| [api/](api/) | Query/ingest interfaces for non-CLI consumers | reserved |
| [storage/](storage/) | Storage engine internals, beyond the schema in `architecture/DATABASE.md` | reserved |
| [search/](search/) | Ranking and retrieval internals, once past plain full-text (Milestone 2) | reserved |
| [sdk/](sdk/) | Client library docs, once `pkg/` has one | reserved |
| [plugins/](plugins/) | Extension points for new parsers/sources | reserved |
| [examples/](examples/) | Doc-level usage walkthroughs (see also root [`../examples/`](../examples/) for runnable code) | reserved |

"Reserved" folders hold a one-line README explaining what will eventually
live there — no content yet, so there's nowhere to accidentally sprawl.
