package memory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/truelogics/ai-memory/internal/chunker"
	fscollector "github.com/truelogics/ai-memory/internal/collector/filesystem"
	"github.com/truelogics/ai-memory/internal/domain"
	"github.com/truelogics/ai-memory/internal/graph"
	"github.com/truelogics/ai-memory/internal/indexer"
	"github.com/truelogics/ai-memory/internal/kernel"
	"github.com/truelogics/ai-memory/internal/normalizer"
	mdparser "github.com/truelogics/ai-memory/internal/parser/markdown"
	"github.com/truelogics/ai-memory/internal/retriever"
	"github.com/truelogics/ai-memory/internal/search"
	"github.com/truelogics/ai-memory/internal/storage/sqlite"
)

const (
	dbDirName  = ".eng"
	dbFileName = "memory.db"

	// defaultWorkspaceID stands in for a real `workspaces` table, which
	// v1's schema doesn't have (see DATABASE.md) — one SQLite file is
	// one Workspace, so there's nothing to disambiguate yet.
	defaultWorkspaceID = "default"
)

// Memory is an open Workspace — the SDK's single entry point (RFC-0004).
// Construct via Open. There is no separate Workspace type: Memory *is*
// the open workspace handle.
type Memory struct {
	store     kernel.Storage
	indexer   kernel.Indexer
	search    kernel.Search
	retriever kernel.Retriever
}

// Open opens (creating if necessary) the workspace rooted at dir — a
// `.eng/memory.db` SQLite file, same convention `eng init` uses. Wires
// every kernel component exactly once; callers never see Collector,
// Parser, Chunker, or any other internal/ type.
func Open(dir string) (*Memory, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	path := filepath.Join(absDir, dbDirName, dbFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("memory: create %s: %w", dbDirName, err)
	}
	store, err := sqlite.Open(path)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}

	idx := indexer.New(
		fscollector.New(),
		[]kernel.Parser{mdparser.New()},
		normalizer.New(),
		chunker.New(chunker.StrategyHeading),
		store,
		graph.NewResolver(store),
	)
	hybridSearch := &search.Search{Storage: store, Graph: graph.New(store)}

	return &Memory{
		store:     store,
		indexer:   idx,
		search:    hybridSearch,
		retriever: &retriever.Retriever{Search: hybridSearch, Storage: store},
	}, nil
}

// Close releases the underlying storage handle.
func (m *Memory) Close() error {
	return m.store.Close()
}

// AddRepository registers path as a Repository in this Workspace. Safe to
// call again for an already-registered path — returns the existing
// registration rather than erroring or duplicating it.
func (m *Memory) AddRepository(ctx context.Context, path string) (Repository, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Repository{}, fmt.Errorf("memory: %w", err)
	}

	existing, ok, err := m.store.FindRepositoryByPath(ctx, absPath)
	if err != nil {
		return Repository{}, fmt.Errorf("memory: %w", err)
	}
	if ok {
		return toRepository(existing), nil
	}

	repo, err := domain.NewRepository(defaultWorkspaceID, filepath.Base(absPath), absPath)
	if err != nil {
		return Repository{}, fmt.Errorf("memory: %w", err)
	}
	if remote := gitRemote(absPath); remote != "" {
		repo = repo.WithRemote(remote)
	}
	if err := m.store.PutRepository(ctx, repo); err != nil {
		return Repository{}, fmt.Errorf("memory: %w", err)
	}
	return toRepository(repo), nil
}

// Index runs a full index of repo (see kernel.Indexer.Index).
func (m *Memory) Index(ctx context.Context, repo Repository) (IndexResult, error) {
	domainRepo, err := m.store.GetRepository(ctx, repo.ID)
	if err != nil {
		return IndexResult{}, fmt.Errorf("memory: %w", err)
	}
	result, err := m.indexer.Index(ctx, domainRepo)
	if err != nil {
		return IndexResult{}, fmt.Errorf("memory: %w", err)
	}
	return toIndexResult(result), nil
}

// Sync incrementally re-indexes repo using git as the source of truth
// for what changed (see kernel.Indexer.Sync) — falls back to a full
// Index when repo has never been indexed or isn't a git repository.
func (m *Memory) Sync(ctx context.Context, repo Repository) (IndexResult, error) {
	domainRepo, err := m.store.GetRepository(ctx, repo.ID)
	if err != nil {
		return IndexResult{}, fmt.Errorf("memory: %w", err)
	}
	result, err := m.indexer.Sync(ctx, domainRepo)
	if err != nil {
		return IndexResult{}, fmt.Errorf("memory: %w", err)
	}
	return toIndexResult(result), nil
}

// Search runs a ranked, hybrid full-text query (see internal/search).
func (m *Memory) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	// query is whatever a human or a model typed, so it is escaped
	// rather than handed to FTS5 as an expression: a hyphen in it is
	// otherwise a syntax error, not a search.
	fts, ok := search.UserQuery(query)
	if !ok {
		return nil, nil
	}
	results, err := m.search.Search(ctx, fts, kernel.SearchOptions{
		RepositoryID: opts.RepositoryID,
		Limit:        opts.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}

	repoNames, err := m.repositoryNames(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]SearchResult, len(results))
	for i, r := range results {
		related := make([]string, len(r.Related))
		for j, rel := range r.Related {
			related[j] = rel.Path
		}
		out[i] = SearchResult{
			Path:       r.Document.Path,
			Repository: repoNames[r.Document.RepositoryID],
			Score:      r.Score,
			Snippet:    r.Snippet,
			Related:    related,
		}
	}
	return out, nil
}

// repositoryNames maps repository id to name, so a result can say which
// registered Repository it came from. A Workspace holds many
// repositories and a document path is only unique within one of them.
func (m *Memory) repositoryNames(ctx context.Context) (map[string]string, error) {
	repos, err := m.store.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	names := make(map[string]string, len(repos))
	for _, r := range repos {
		names[r.ID] = r.Name
	}
	return names, nil
}

// Context gathers and assembles context for task — Retriever's grouped
// bundle, reshaped into a ContextPackage (RFC-0004). task can be a
// question ("how does authentication work?") or a broader description
// ("Review authentication PR") — Retriever treats both the same way.
func (m *Memory) Context(ctx context.Context, task string) (ContextPackage, error) {
	return m.ContextFor(ctx, task, ContextOptions{})
}

// ContextFor is Context with options — today, the paths a task concerns,
// so the Rules section can be scoped to the rules that actually govern
// them (RFC-0005). Without them a review of Go files can be handed a
// TypeScript rule, which is what the first cross-repository retrieval
// this kernel ever performed actually did.
//
// A separate method rather than a changed signature: adding one is not a
// breaking change, so every existing caller keeps compiling and behaving
// identically (KERNEL_POLICY.md Rule #2).
func (m *Memory) ContextFor(ctx context.Context, task string, opts ContextOptions) (ContextPackage, error) {
	bundle, err := m.retriever.Retrieve(ctx, task, kernel.RetrieveOptions{ChangedPaths: opts.ChangedPaths})
	if err != nil {
		return ContextPackage{}, fmt.Errorf("memory: %w", err)
	}
	repoNames, err := m.repositoryNames(ctx)
	if err != nil {
		return ContextPackage{}, err
	}
	return toContextPackage(bundle, repoNames), nil
}

// gitRemote best-effort reads dir's `origin` remote; empty on any error.
func gitRemote(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
