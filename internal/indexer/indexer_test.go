package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	mdchunker "github.com/truelogics/engineering-kernel/internal/chunker"
	fscollector "github.com/truelogics/engineering-kernel/internal/collector/filesystem"
	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/graph"
	"github.com/truelogics/engineering-kernel/internal/kernel"
	mdnormalizer "github.com/truelogics/engineering-kernel/internal/normalizer"
	mdparser "github.com/truelogics/engineering-kernel/internal/parser/markdown"
	"github.com/truelogics/engineering-kernel/internal/storage/sqlite"
)

// --- stubs for isolated control-flow tests ---

type stubCollector struct {
	docs []domain.RawDocument
	err  error
}

func (c *stubCollector) Collect(ctx context.Context, repo domain.Repository) ([]domain.RawDocument, error) {
	return c.docs, c.err
}

type stubParser struct {
	fail map[string]bool // paths that should fail to parse
}

func (p *stubParser) CanParse(raw domain.RawDocument) bool { return true }

func (p *stubParser) Parse(ctx context.Context, raw domain.RawDocument) (domain.CanonicalDocument, error) {
	if p.fail[raw.Path] {
		return domain.CanonicalDocument{}, fmt.Errorf("stub parser: forced failure for %s", raw.Path)
	}
	doc, err := domain.NewCanonicalDocument(raw.SourceID, raw.SourceID, raw.Path)
	if err != nil {
		return domain.CanonicalDocument{}, err
	}
	doc.Content = string(raw.Bytes)
	doc.ContentHash = string(raw.Bytes) // deterministic stand-in for a real hash, keyed to content
	return doc, nil
}

type refusingParser struct{}

func (refusingParser) CanParse(raw domain.RawDocument) bool { return false }
func (refusingParser) Parse(context.Context, domain.RawDocument) (domain.CanonicalDocument, error) {
	return domain.CanonicalDocument{}, errors.New("should never be called")
}

func openTestStore(t *testing.T) kernel.Storage {
	t.Helper()
	store, err := sqlite.Open("file:" + t.Name() + "?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func testRepo(t *testing.T, storage kernel.Storage) domain.Repository {
	t.Helper()
	repo, err := domain.NewRepository("ws-1", "test-repo", "/repos/test-repo")
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	if err := storage.PutRepository(context.Background(), repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}
	return repo
}

func TestIndexAddsNewDocuments(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{docs: []domain.RawDocument{
		{SourceID: repo.ID, Path: "a.md", Bytes: []byte("content a")},
		{SourceID: repo.ID, Path: "b.md", Bytes: []byte("content b")},
	}}
	idx := New(collector, []kernel.Parser{&stubParser{}}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)

	result, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index: unexpected error: %v", err)
	}
	if result.Scanned != 2 || result.Added != 2 || result.Updated != 0 || result.Unchanged != 0 || result.Errors != 0 {
		t.Fatalf("Index result = %+v, want Scanned=2 Added=2", result)
	}

	docs, err := storage.ListDocuments(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("ListDocuments after Index = %d docs, want 2", len(docs))
	}
}

func TestIndexSkipsUnchangedOnSecondRun(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{docs: []domain.RawDocument{
		{SourceID: repo.ID, Path: "a.md", Bytes: []byte("stable content")},
	}}
	idx := New(collector, []kernel.Parser{&stubParser{}}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)

	if _, err := idx.Index(ctx, repo); err != nil {
		t.Fatalf("Index (first run): %v", err)
	}
	result, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index (second run): %v", err)
	}
	if result.Unchanged != 1 || result.Added != 0 || result.Updated != 0 {
		t.Fatalf("second Index result = %+v, want Unchanged=1", result)
	}
}

func TestIndexReportsUpdatedWhenContentChanges(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{docs: []domain.RawDocument{
		{SourceID: repo.ID, Path: "a.md", Bytes: []byte("version one")},
	}}
	idx := New(collector, []kernel.Parser{&stubParser{}}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)
	if _, err := idx.Index(ctx, repo); err != nil {
		t.Fatalf("Index (first run): %v", err)
	}

	collector.docs[0].Bytes = []byte("version two, changed")
	result, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index (second run): %v", err)
	}
	if result.Updated != 1 || result.Added != 0 || result.Unchanged != 0 {
		t.Fatalf("Index result after content change = %+v, want Updated=1", result)
	}
}

func TestIndexCountsErrorsAndContinuesProcessingOtherFiles(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{docs: []domain.RawDocument{
		{SourceID: repo.ID, Path: "good.md", Bytes: []byte("fine")},
		{SourceID: repo.ID, Path: "bad.md", Bytes: []byte("boom")},
	}}
	parser := &stubParser{fail: map[string]bool{"bad.md": true}}
	idx := New(collector, []kernel.Parser{parser}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)

	result, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index: unexpected top-level error (should count per-file errors instead): %v", err)
	}
	if result.Errors != 1 || result.Added != 1 {
		t.Fatalf("Index result = %+v, want Errors=1 Added=1", result)
	}
}

func TestIndexNoParserMatchCountsAsError(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{docs: []domain.RawDocument{
		{SourceID: repo.ID, Path: "unknown.xyz", Bytes: []byte("???")},
	}}
	idx := New(collector, []kernel.Parser{refusingParser{}}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)

	result, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index: unexpected error: %v", err)
	}
	if result.Errors != 1 {
		t.Fatalf("Index result = %+v, want Errors=1 (no parser matched)", result)
	}
}

func TestIndexPropagatesCollectorError(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{err: errors.New("disk on fire")}
	idx := New(collector, []kernel.Parser{&stubParser{}}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)

	if _, err := idx.Index(ctx, repo); err == nil {
		t.Fatal("Index: expected error when Collector fails")
	}
}

func TestIndexWritesIndexState(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo := testRepo(t, storage)

	collector := &stubCollector{docs: []domain.RawDocument{
		{SourceID: repo.ID, Path: "a.md", Bytes: []byte("content")},
	}}
	fixedNow := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	idx := New(collector, []kernel.Parser{&stubParser{}}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)
	idx.Now = func() time.Time { return fixedNow }

	if _, err := idx.Index(ctx, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	state, ok, err := storage.GetIndexState(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetIndexState: %v", err)
	}
	if !ok {
		t.Fatal("GetIndexState: expected index state to exist after Index")
	}
	if state.DocumentCount != 1 || state.Status != kernel.IndexStatusClean || !state.LastFullIndexAt.Equal(fixedNow) {
		t.Fatalf("GetIndexState = %+v, want DocumentCount=1 Status=clean LastFullIndexAt=%v", state, fixedNow)
	}
}

// --- end-to-end: real Collector + real Parser + real Normalizer + real
// Chunker + real sqlite Storage, on files written to a temp directory ---

func TestIndexEndToEndWithRealComponents(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "---\ndoc: README\nstatus: living\n---\n\n# Demo Repo\n\nWe chose JWT for stateless authentication.\n")
	writeFile(t, dir, "ARCHITECTURE.md", "# Architecture\n\nThe pipeline has several stages.\n")

	storage := openTestStore(t)
	repo, err := domain.NewRepository("ws-1", "demo-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	if err := storage.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	idx := New(
		fscollector.New(),
		[]kernel.Parser{mdparser.New()},
		mdnormalizer.New(),
		mdchunker.New(mdchunker.StrategyHeading),
		storage,
		graph.NewResolver(storage),
	)

	result, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index: unexpected error: %v", err)
	}
	if result.Scanned != 2 || result.Added != 2 || result.Errors != 0 {
		t.Fatalf("Index result = %+v, want Scanned=2 Added=2 Errors=0", result)
	}

	matches, err := storage.SearchChunks(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(matches) != 1 || matches[0].Document.Path != "README.md" {
		t.Fatalf("SearchChunks(authentication) = %+v, want 1 hit on README.md", matches)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial")
}

// TestIndexExtractsRelationships verifies graph.Extract is actually wired
// into Index (Milestone 1): an RFC that supersedes an earlier one, real
// components end to end, should produce a stored Relationship.
func TestIndexExtractsRelationships(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "rfcs/0001-old.md", "---\ndoc: RFC\n---\n\n# Old RFC\n\nOriginal content.\n")
	writeFile(t, dir, "rfcs/0002-new.md", "---\ndoc: RFC\nsupersedes: 0001\n---\n\n# New RFC\n\nReplacement content.\n")

	storage := openTestStore(t)
	repo, err := domain.NewRepository("ws-1", "demo-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	if err := storage.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	idx := New(fscollector.New(), []kernel.Parser{mdparser.New()}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, graph.NewResolver(storage))
	if _, err := idx.Index(ctx, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	newDocID := domain.DocumentID(repo.ID, "rfcs/0002-new.md")
	oldDocID := domain.DocumentID(repo.ID, "rfcs/0001-old.md")
	rels, err := storage.ListRelationships(ctx, newDocID)
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(rels) != 1 || rels[0].ToDocumentID != oldDocID || rels[0].Type != domain.RelationshipSupersedes {
		t.Fatalf("ListRelationships(new doc) = %+v, want 1 supersedes edge to the old RFC", rels)
	}
}

// TestSyncEndToEnd exercises Milestone 3 against a real git repo: an
// initial full Index, then a modify + an add + a delete, then Sync
// should only touch the changed files and remove the deleted one.
func TestSyncEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Demo\n\nOriginal content.\n")
	writeFile(t, dir, "REMOVE_ME.md", "# Going away\n")
	initGitRepo(t, dir)

	storage := openTestStore(t)
	repo, err := domain.NewRepository("ws-1", "demo-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	if err := storage.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	idx := New(fscollector.New(), []kernel.Parser{mdparser.New()}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, graph.NewResolver(storage))

	initial, err := idx.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if initial.Scanned != 2 || initial.Added != 2 {
		t.Fatalf("initial Index = %+v, want Scanned=2 Added=2", initial)
	}

	repo, err = storage.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.LastIndexedCommit == "" {
		t.Fatal("expected Index to record LastIndexedCommit via IncrementalCollector.CurrentRef")
	}

	// Modify README, add a new file, delete REMOVE_ME.md, commit.
	writeFile(t, dir, "README.md", "# Demo\n\nUpdated content about authentication.\n")
	writeFile(t, dir, "NEW.md", "# Brand new\n")
	if err := os.Remove(filepath.Join(dir, "REMOVE_ME.md")); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "second commit")

	syncResult, err := idx.Sync(ctx, repo)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if syncResult.Scanned != 2 {
		t.Fatalf("Sync result = %+v, want Scanned=2 (README.md modified + NEW.md added, not REMOVE_ME.md which was deleted)", syncResult)
	}
	if syncResult.Updated != 1 || syncResult.Added != 1 || syncResult.Deleted != 1 {
		t.Fatalf("Sync result = %+v, want Updated=1 Added=1 Deleted=1", syncResult)
	}

	docs, err := storage.ListDocuments(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("ListDocuments after Sync = %d docs, want 2 (README.md, NEW.md — REMOVE_ME.md gone)", len(docs))
	}

	state, ok, err := storage.GetIndexState(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetIndexState: %v", err)
	}
	if !ok || state.DocumentCount != 2 || state.LastIncrementalIndexAt.IsZero() {
		t.Fatalf("GetIndexState after Sync = %+v, want DocumentCount=2 and LastIncrementalIndexAt set", state)
	}
	if state.LastFullIndexAt.IsZero() {
		t.Fatal("Sync must preserve LastFullIndexAt from the earlier Index run, not zero it out")
	}

	matches, err := storage.SearchChunks(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("SearchChunks(authentication) after Sync = %+v, want the updated README content indexed", matches)
	}
}

// TestSyncFallsBackToIndexWithoutPriorCommit verifies Sync degrades
// gracefully to a full Index when LastIndexedCommit is empty (repo has
// never been indexed).
func TestSyncFallsBackToIndexWithoutPriorCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Demo\n")
	initGitRepo(t, dir)

	storage := openTestStore(t)
	repo, err := domain.NewRepository("ws-1", "demo-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	if err := storage.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	idx := New(fscollector.New(), []kernel.Parser{mdparser.New()}, mdnormalizer.New(), mdchunker.New(mdchunker.StrategyHeading), storage, nil)
	result, err := idx.Sync(ctx, repo)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Scanned != 1 || result.Added != 1 {
		t.Fatalf("Sync (no prior commit) = %+v, want a full Index (Scanned=1 Added=1)", result)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
