package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// Collector fetches raw bytes from a Repository. v1 has one
// implementation (internal/collector/filesystem) that both enumerates and
// reads local files; a future non-filesystem Collector (GitHub, Slack,
// Notion) implements the same interface without Indexer, Parser,
// Normalizer, Chunker, Storage, or Search changing. See INTERFACES.md.
type Collector interface {
	// Collect returns every RawDocument collectible from repo.
	Collect(ctx context.Context, repo domain.Repository) ([]domain.RawDocument, error)
}

// IncrementalCollector is an optional capability a Collector may
// implement — RFC-0003/GRAPH.md's `eng sync`. Indexer.Sync type-asserts
// for this and falls back to a full Index when a Collector doesn't
// implement it (e.g. not a git repository).
type IncrementalCollector interface {
	// CollectChanged returns RawDocuments changed since sinceCommit
	// (Repository.LastIndexedCommit), plus repo-relative paths deleted
	// since then. Must include working-tree changes (staged and
	// unstaged), not just committed ones — see RFC-0003's Trade-offs.
	CollectChanged(ctx context.Context, repo domain.Repository, sinceCommit string) (changed []domain.RawDocument, deleted []string, err error)

	// CurrentRef returns the repository's current commit, to record as
	// the new Repository.LastIndexedCommit after a successful Index or
	// Sync. Empty string, nil error if not applicable (not a git repo).
	CurrentRef(ctx context.Context, repo domain.Repository) (string, error)
}
