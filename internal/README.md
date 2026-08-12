---
doc: internal
audience: [human, agent]
status: living
owner: engineering-kernel
last_reviewed: 2026-08-02
---

# internal/

Private implementation. Not importable outside this module.

## Map

```
internal/
├── domain/               Core models: Workspace, Repository, Source, Document,
│                          Chunk, Metadata, Tag, Relationship. No I/O, no logic
│                          beyond validation — see DOMAIN_MODEL.md.
├── kernel/                The ten interfaces from INTERFACES.md, with real
│                          method signatures now that Sprint 2 implements them.
├── collector/filesystem/  kernel.Collector — walks a local repo for markdown.
├── parser/markdown/       kernel.Parser — goldmark-based, front-matter + AST.
├── normalizer/            kernel.Normalizer — defaults, dedup, path cleaning.
├── chunker/               kernel.Chunker — heading/paragraph/fixed-size.
├── storage/sqlite/        kernel.Storage — schema, CRUD, FTS5, transactions.
├── search/                kernel.Search — ranking + related-document lookup.
├── indexer/               kernel.Indexer — orchestrates the above pipeline.
└── cli/                   `eng init/index/search/status`, callable without
                           going through cmd/eng — see cmd/README.md.
```

Every implementation package has a `var _ kernel.X = (*Y)(nil)` assertion
tying it back to its interface in `kernel/`.
