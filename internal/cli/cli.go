// Package cli implements the four commands Step 7's Definition of Done
// requires — init, index, search, status — wiring the real kernel
// components together. See docs/cli/CLI.md.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/truelogics/ai-memory/internal/chunker"
	fscollector "github.com/truelogics/ai-memory/internal/collector/filesystem"
	"github.com/truelogics/ai-memory/internal/contextbuilder"
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
	// one Workspace in v1, so there's nothing to disambiguate yet.
	defaultWorkspaceID = "default"
)

func dbPath(dir string) string {
	return filepath.Join(dir, dbDirName, dbFileName)
}

// openStore opens the workspace database for dir, requiring it to
// already exist (i.e. `eng init` has run) unless create is true.
func openStore(dir string, create bool) (*sqlite.Store, error) {
	path := dbPath(dir)
	if !create {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("no workspace at %s — run `eng init` first", dir)
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dbDirName, err)
	}
	return sqlite.Open(path)
}

// gitRemote best-effort reads dir's `origin` remote; empty on any error.
func gitRemote(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// findOrCreateRepository registers dir as a Repository if it isn't
// already, returning whether it already existed.
func findOrCreateRepository(ctx context.Context, store kernel.Storage, dir string) (domain.Repository, bool, error) {
	existing, ok, err := store.FindRepositoryByPath(ctx, dir)
	if err != nil {
		return domain.Repository{}, false, err
	}
	if ok {
		return existing, true, nil
	}
	repo, err := domain.NewRepository(defaultWorkspaceID, filepath.Base(dir), dir)
	if err != nil {
		return domain.Repository{}, false, err
	}
	return repo, false, nil
}

func newIndexer(store kernel.Storage) *indexer.Indexer {
	return indexer.New(
		fscollector.New(),
		[]kernel.Parser{mdparser.New()},
		normalizer.New(),
		chunker.New(chunker.StrategyHeading),
		store,
		graph.NewResolver(store),
	)
}

// Init implements `eng init`: creates .eng/memory.db and registers dir's
// repository.
func Init(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	store, err := openStore(absDir, true)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	defer store.Close()

	repo, existed, err := findOrCreateRepository(ctx, store, absDir)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	if remote := gitRemote(absDir); remote != "" {
		repo = repo.WithRemote(remote)
	}
	if err := store.PutRepository(ctx, repo); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	fmt.Fprintf(out, "Created workspace at %s\n", dbPath(absDir))
	verb := "Registered"
	if existed {
		verb = "Already registered"
	}
	fmt.Fprintf(out, "%s repository: %s (%s)\n", verb, repo.Name, absDir)
	return nil
}

// Index implements `eng index [path]`: runs the full Collector -> Parser
// -> Normalizer -> Chunker -> Storage pipeline for every repository
// attached to the workspace at dir.
//
// This indexes the whole workspace, not just dir. Indexing only dir made
// `eng workspace attach` pointless the moment anyone re-indexed: a
// second attached repository would keep whatever documents it had from
// attach time and silently never be refreshed again. The workspace is
// the indexing boundary, so it is also the re-indexing boundary.
func Index(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	defer store.Close()

	repos, err := workspaceRepositories(ctx, store, absDir)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}

	idx := newIndexer(store)
	for _, repo := range repos {
		result, err := idx.Index(ctx, repo)
		if err != nil {
			return fmt.Errorf("index %s: %w", repo.Name, err)
		}
		fmt.Fprintf(out, "%s: %d scanned, %d added, %d updated, %d unchanged, %d errors\n",
			repo.Name, result.Scanned, result.Added, result.Updated, result.Unchanged, result.Errors)
		printFailures(out, result.Failures)
	}
	return nil
}

// workspaceRepositories returns every repository attached to the
// workspace, registering absDir itself when the workspace is empty —
// which keeps `eng init && eng index .` working unchanged for the
// single-repository case that was the only case before workspaces had a
// CLI.
func workspaceRepositories(ctx context.Context, store kernel.Storage, absDir string) ([]domain.Repository, error) {
	repos, err := store.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}
	if len(repos) > 0 {
		return repos, nil
	}

	repo, _, err := findOrCreateRepository(ctx, store, absDir)
	if err != nil {
		return nil, err
	}
	if err := store.PutRepository(ctx, repo); err != nil {
		return nil, err
	}
	return []domain.Repository{repo}, nil
}

// Sync implements `eng sync [path]`: incremental re-index using git as
// the source of truth for what changed — RFC-0003/GRAPH.md. Falls back
// to a full Index when the repo has never been indexed or isn't a git
// repository (Indexer.Sync's own fallback).
func Sync(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	defer store.Close()

	repo, _, err := findOrCreateRepository(ctx, store, absDir)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := store.PutRepository(ctx, repo); err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	result, err := newIndexer(store).Sync(ctx, repo)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	fmt.Fprintf(out, "%s: %d scanned, %d added, %d updated, %d unchanged, %d deleted, %d errors\n",
		repo.Name, result.Scanned, result.Added, result.Updated, result.Unchanged, result.Deleted, result.Errors)
	printFailures(out, result.Failures)
	return nil
}

// Search implements `eng search <query>`.
func Search(ctx context.Context, dir, query string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	defer store.Close()

	fts, ok := search.UserQuery(query)
	if !ok {
		fmt.Fprintln(out, "No matches.")
		return nil
	}

	hybrid := &search.Search{Storage: store, Graph: graph.New(store)}
	results, err := hybrid.Search(ctx, fts, kernel.SearchOptions{Limit: 10})
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		fmt.Fprintln(out, "No matches.")
		return nil
	}
	for i, r := range results {
		fmt.Fprintf(out, "%d. %-40s score %.2f\n", i+1, r.Document.Path, r.Score)
		if r.Snippet != "" {
			fmt.Fprintf(out, "   %q\n", r.Snippet)
		}
		if len(r.Related) > 0 {
			names := make([]string, len(r.Related))
			for j, rel := range r.Related {
				names[j] = rel.Path
			}
			fmt.Fprintf(out, "   related: %s\n", strings.Join(names, ", "))
		}
	}
	return nil
}

// Context implements `eng context --task "<description>"` (Step 8's
// Definition of Done) and `eng ask <question>` (CLI.md's original
// design) — the same Retriever -> ContextBuilder pipeline either way; a
// "task" and a "question" are the same input shape as far as Retriever
// is concerned (ARCHITECTURE.md's Retriever docs).
func Context(ctx context.Context, dir, task string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("context: %w", err)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("context: %w", err)
	}
	defer store.Close()

	hybrid := &search.Search{Storage: store, Graph: graph.New(store)}
	bundle, err := (&retriever.Retriever{Search: hybrid, Storage: store}).Retrieve(ctx, task, kernel.RetrieveOptions{})
	if err != nil {
		return fmt.Errorf("context: %w", err)
	}
	assembled, err := contextbuilder.New().Build(ctx, bundle)
	if err != nil {
		return fmt.Errorf("context: %w", err)
	}

	fmt.Fprintln(out, assembled.Body)
	return nil
}

// Status implements `eng status [path]`.
func Status(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	defer store.Close()

	repos, err := store.ListRepositories(ctx)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if len(repos) == 0 {
		fmt.Fprintln(out, "No repositories registered. Run `eng index .` first.")
		return nil
	}

	fmt.Fprintf(out, "%-20s %6s  %-19s  %s\n", "REPOSITORY", "DOCS", "LAST INDEXED", "STATUS")
	for _, repo := range repos {
		state, ok, err := store.GetIndexState(ctx, repo.ID)
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		status, docs, lastIndexed := "not indexed", 0, "-"
		if ok {
			status = string(state.Status)
			docs = state.DocumentCount
			// Whichever of the two is more recent — a Sync run updates
			// LastIncrementalIndexAt without touching LastFullIndexAt.
			last := state.LastFullIndexAt
			if state.LastIncrementalIndexAt.After(last) {
				last = state.LastIncrementalIndexAt
			}
			if !last.IsZero() {
				lastIndexed = last.Format("2006-01-02 15:04:05")
			}
		}
		fmt.Fprintf(out, "%-20s %6d  %-19s  %s\n", repo.Name, docs, lastIndexed, status)
	}
	return nil
}

// --- Workspace commands ---------------------------------------------
//
// A Workspace is the indexing boundary: one `.eng/memory.db` holding one
// or more registered repositories, searched together. Until Sprint 7
// this was reachable only from Go (`pkg/memory.AddRepository`), never
// from the CLI, so in practice every workspace held exactly one
// repository — the directory it was created in. That is why a review of
// an application repository could never retrieve the engineering
// knowledge base's rules: not because the kernel couldn't hold both, but
// because nothing let anyone say so.

// WorkspaceCreate implements `eng workspace create [path]` — the
// workspace-vocabulary spelling of `eng init`. Creating a workspace and
// registering its own directory is the same operation; this name says
// what is being made rather than what is being bootstrapped.
func WorkspaceCreate(ctx context.Context, dir string, out io.Writer) error {
	return Init(ctx, dir, out)
}

// WorkspaceList implements `eng workspace list`: every repository
// registered in the workspace at dir, with its indexed document count.
func WorkspaceList(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("workspace list: %w", err)
	}
	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("workspace list: %w", err)
	}
	defer store.Close()

	repos, err := store.ListRepositories(ctx)
	if err != nil {
		return fmt.Errorf("workspace list: %w", err)
	}
	if len(repos) == 0 {
		fmt.Fprintf(out, "No repositories registered in %s\n", dbPath(absDir))
		return nil
	}

	fmt.Fprintf(out, "Workspace: %s\n\n", dbPath(absDir))
	for _, repo := range repos {
		docs, err := store.ListDocuments(ctx, repo.ID)
		if err != nil {
			return fmt.Errorf("workspace list: %w", err)
		}
		fmt.Fprintf(out, "  %-20s %4d documents  %s\n", repo.Name, len(docs), repo.LocalPath)
	}
	return nil
}

// WorkspaceAttach implements `eng workspace attach <path>`: registers
// repoPath into the workspace at dir and indexes it immediately. Attach
// indexes rather than only registering because a registered-but-unindexed
// repository is indistinguishable, at retrieval time, from one that
// isn't there — it contributes nothing and reports no error.
func WorkspaceAttach(ctx context.Context, dir, repoPath string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("workspace attach: %w", err)
	}
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("workspace attach: %w", err)
	}
	if info, err := os.Stat(absRepo); err != nil || !info.IsDir() {
		return fmt.Errorf("workspace attach: %s is not a directory", absRepo)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("workspace attach: %w", err)
	}
	defer store.Close()

	repo, existed, err := findOrCreateRepository(ctx, store, absRepo)
	if err != nil {
		return fmt.Errorf("workspace attach: %w", err)
	}
	if remote := gitRemote(absRepo); remote != "" {
		repo = repo.WithRemote(remote)
	}
	if err := store.PutRepository(ctx, repo); err != nil {
		return fmt.Errorf("workspace attach: %w", err)
	}

	result, err := newIndexer(store).Index(ctx, repo)
	if err != nil {
		return fmt.Errorf("workspace attach: %w", err)
	}

	verb := "Attached"
	if existed {
		verb = "Re-indexed already-attached"
	}
	fmt.Fprintf(out, "%s %s (%s)\n", verb, repo.Name, absRepo)
	fmt.Fprintf(out, "  %d scanned, %d added, %d updated, %d unchanged, %d errors\n",
		result.Scanned, result.Added, result.Updated, result.Unchanged, result.Errors)
	printFailures(out, result.Failures)
	return nil
}

// WorkspaceDetach implements `eng workspace detach <path>`: removes
// repoPath's documents and its registration from the workspace at dir.
// Documents are deleted before the registration, so an interrupted
// detach leaves a registered repository with missing documents — which
// `eng index` repairs — rather than orphaned documents no repository
// claims, which nothing would.
func WorkspaceDetach(ctx context.Context, dir, repoPath string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("workspace detach: %w", err)
	}
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("workspace detach: %w", err)
	}

	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("workspace detach: %w", err)
	}
	defer store.Close()

	repo, ok, err := store.FindRepositoryByPath(ctx, absRepo)
	if err != nil {
		return fmt.Errorf("workspace detach: %w", err)
	}
	if !ok {
		return fmt.Errorf("workspace detach: %s is not attached to this workspace", absRepo)
	}

	docs, err := store.ListDocuments(ctx, repo.ID)
	if err != nil {
		return fmt.Errorf("workspace detach: %w", err)
	}
	for _, doc := range docs {
		if err := store.DeleteDocument(ctx, doc.ID); err != nil {
			return fmt.Errorf("workspace detach: %w", err)
		}
	}
	if err := store.DeleteRepository(ctx, repo.ID); err != nil {
		return fmt.Errorf("workspace detach: %w", err)
	}

	fmt.Fprintf(out, "Detached %s (%s): %d documents removed\n", repo.Name, absRepo, len(docs))
	return nil
}

// printFailures names the files an index run could not process. A bare
// error count is unactionable: it says something is wrong and gives a
// developer nowhere to start. Capped, because a broken parser would
// otherwise print one line per file in the repository.
func printFailures(out io.Writer, failures []kernel.IndexFailure) {
	const shown = 10
	for i, f := range failures {
		if i == shown {
			fmt.Fprintf(out, "  ... and %d more\n", len(failures)-shown)
			break
		}
		fmt.Fprintf(out, "  ! %s: %s\n", f.Path, f.Reason)
	}
}
