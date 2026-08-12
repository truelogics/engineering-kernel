package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	// A unique in-memory DB per test (not a shared ":memory:" DSN, which
	// modernc.org/sqlite would otherwise let tests bleed into each other).
	store, err := Open("file:" + t.Name() + "?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestOpenAppliesSchemaIdempotently(t *testing.T) {
	store := openTestStore(t)
	// Re-applying the schema on an already-open store must not error.
	if _, err := store.db.Exec(schema); err != nil {
		t.Fatalf("re-applying schema failed: %v", err)
	}
}

func TestRepositoryCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, err := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	repo = repo.WithRemote("git@github.com:truelogics/ai-memory.git")

	if err := store.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	got, err := store.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if got != repo {
		t.Fatalf("GetRepository = %+v, want %+v", got, repo)
	}

	found, ok, err := store.FindRepositoryByPath(ctx, repo.LocalPath)
	if err != nil {
		t.Fatalf("FindRepositoryByPath: %v", err)
	}
	if !ok || found.ID != repo.ID {
		t.Fatalf("FindRepositoryByPath: got (%+v, %v), want repo found", found, ok)
	}

	_, ok, err = store.FindRepositoryByPath(ctx, "/does/not/exist")
	if err != nil {
		t.Fatalf("FindRepositoryByPath (miss): %v", err)
	}
	if ok {
		t.Fatal("FindRepositoryByPath: expected ok=false for unknown path")
	}

	// Upsert: PutRepository again with a changed field.
	updated := repo.MarkIndexed("abc123", time.Now().UTC().Truncate(time.Second))
	if err := store.PutRepository(ctx, updated); err != nil {
		t.Fatalf("PutRepository (update): %v", err)
	}
	got, err = store.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepository (after update): %v", err)
	}
	if got.LastIndexedCommit != "abc123" {
		t.Fatalf("GetRepository after update: LastIndexedCommit = %q, want %q", got.LastIndexedCommit, "abc123")
	}

	list, err := store.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListRepositories: got %d, want 1", len(list))
	}
}

func TestGetRepositoryNotFound(t *testing.T) {
	store := openTestStore(t)
	if _, err := store.GetRepository(context.Background(), "missing"); err == nil {
		t.Fatal("GetRepository: expected error for missing id")
	}
}

func newTestDoc(t *testing.T, repoID, path string) domain.CanonicalDocument {
	t.Helper()
	doc, err := domain.NewCanonicalDocument(repoID, repoID, path)
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Title = path
	doc.Content = "Some content about " + path
	doc.Metadata.Set("status", "living")
	tag, _ := domain.NewTag(doc.ID, "audience", "human")
	doc.Tags = []domain.Tag{tag}
	return doc
}

func TestDocumentCRUDWithTagsAndRelationships(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	if err := store.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	docA := newTestDoc(t, repo.ID, "README.md")
	docB := newTestDoc(t, repo.ID, "ARCHITECTURE.md")
	rel, err := domain.NewRelationship(docA.ID, docB.ID, domain.RelationshipReferences, domain.RelationshipExplicit)
	if err != nil {
		t.Fatalf("NewRelationship: %v", err)
	}
	docA.Relationships = []domain.Relationship{rel}

	if err := store.PutDocument(ctx, docB); err != nil {
		t.Fatalf("PutDocument(docB): %v", err)
	}
	if err := store.PutDocument(ctx, docA); err != nil {
		t.Fatalf("PutDocument(docA): %v", err)
	}

	got, err := store.GetDocument(ctx, docA.ID)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got.Title != docA.Title || got.Content != docA.Content {
		t.Fatalf("GetDocument = %+v, want Title/Content matching %+v", got, docA)
	}
	if v, ok := got.Metadata.Get("status"); !ok || v != "living" {
		t.Fatalf("GetDocument Metadata[status] = (%q, %v)", v, ok)
	}
	if len(got.Tags) != 1 || got.Tags[0].Key != "audience" {
		t.Fatalf("GetDocument Tags = %+v", got.Tags)
	}
	if len(got.Relationships) != 1 || got.Relationships[0].ToDocumentID != docB.ID {
		t.Fatalf("GetDocument Relationships = %+v", got.Relationships)
	}

	byPath, ok, err := store.FindDocumentByPath(ctx, repo.ID, "README.md")
	if err != nil {
		t.Fatalf("FindDocumentByPath: %v", err)
	}
	if !ok || byPath.ID != docA.ID {
		t.Fatalf("FindDocumentByPath: got (%+v, %v)", byPath, ok)
	}

	all, err := store.ListDocuments(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListDocuments: got %d, want 2", len(all))
	}

	// Re-putting docA with no tags/relationships must clear the old ones
	// (replace semantics, not append).
	docA.Tags = nil
	docA.Relationships = nil
	if err := store.PutDocument(ctx, docA); err != nil {
		t.Fatalf("PutDocument (clear tags): %v", err)
	}
	got, err = store.GetDocument(ctx, docA.ID)
	if err != nil {
		t.Fatalf("GetDocument (after clear): %v", err)
	}
	if len(got.Tags) != 0 || len(got.Relationships) != 0 {
		t.Fatalf("expected tags/relationships cleared, got Tags=%+v Relationships=%+v", got.Tags, got.Relationships)
	}
}

func TestFindDocumentByPathNotFound(t *testing.T) {
	store := openTestStore(t)
	_, ok, err := store.FindDocumentByPath(context.Background(), "repo-1", "missing.md")
	if err != nil {
		t.Fatalf("FindDocumentByPath: unexpected error: %v", err)
	}
	if ok {
		t.Fatal("FindDocumentByPath: expected ok=false")
	}
}

func TestChunksCRUDAndReplace(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = store.PutRepository(ctx, repo)
	doc := newTestDoc(t, repo.ID, "README.md")
	if err := store.PutDocument(ctx, doc); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	c0, _ := domain.NewChunk(doc.ID, 0, "Intro", "authentication uses JWT tokens")
	c1, _ := domain.NewChunk(doc.ID, 1, "Details", "more words here")
	if err := store.PutChunks(ctx, doc.ID, []domain.Chunk{c0, c1}); err != nil {
		t.Fatalf("PutChunks: %v", err)
	}

	got, err := store.ListChunks(ctx, doc.ID)
	if err != nil {
		t.Fatalf("ListChunks: %v", err)
	}
	if len(got) != 2 || got[0].Heading != "Intro" || got[1].Heading != "Details" {
		t.Fatalf("ListChunks = %+v", got)
	}

	// Replace with a single chunk — old ones (and their FTS rows) must
	// be gone entirely, not merged.
	c0b, _ := domain.NewChunk(doc.ID, 0, "Only", "replaced content")
	if err := store.PutChunks(ctx, doc.ID, []domain.Chunk{c0b}); err != nil {
		t.Fatalf("PutChunks (replace): %v", err)
	}
	got, err = store.ListChunks(ctx, doc.ID)
	if err != nil {
		t.Fatalf("ListChunks (after replace): %v", err)
	}
	if len(got) != 1 || got[0].Heading != "Only" {
		t.Fatalf("ListChunks after replace = %+v, want 1 chunk 'Only'", got)
	}
}

func TestSearchChunksRanksAndFilters(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repoA, _ := domain.NewRepository("ws-1", "repo-a", "/repos/a")
	repoB, _ := domain.NewRepository("ws-1", "repo-b", "/repos/b")
	_ = store.PutRepository(ctx, repoA)
	_ = store.PutRepository(ctx, repoB)

	docAuth := newTestDoc(t, repoA.ID, "auth.md")
	docAuth.Type = domain.DocTypeADR
	_ = store.PutDocument(ctx, docAuth)
	chunkAuth, _ := domain.NewChunk(docAuth.ID, 0, "Auth", "we chose JWT for stateless authentication across services")
	if err := store.PutChunks(ctx, docAuth.ID, []domain.Chunk{chunkAuth}); err != nil {
		t.Fatalf("PutChunks: %v", err)
	}

	docOther := newTestDoc(t, repoB.ID, "other.md")
	_ = store.PutDocument(ctx, docOther)
	chunkOther, _ := domain.NewChunk(docOther.ID, 0, "Other", "this document is about deployment pipelines")
	if err := store.PutChunks(ctx, docOther.ID, []domain.Chunk{chunkOther}); err != nil {
		t.Fatalf("PutChunks: %v", err)
	}

	results, err := store.SearchChunks(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(results) != 1 || results[0].Document.ID != docAuth.ID {
		t.Fatalf("SearchChunks(authentication) = %+v, want 1 hit in docAuth", results)
	}
	if results[0].Snippet == "" {
		t.Error("expected a non-empty snippet")
	}

	// Filter by repository — searching for a term present in both
	// documents' generic content, restricted to repoB.
	results, err = store.SearchChunks(ctx, "document", kernel.SearchOptions{RepositoryID: repoB.ID})
	if err != nil {
		t.Fatalf("SearchChunks (repo filter): %v", err)
	}
	for _, r := range results {
		if r.Document.RepositoryID != repoB.ID {
			t.Errorf("SearchChunks with RepositoryID filter returned a hit from %s", r.Document.RepositoryID)
		}
	}

	// Filter by doc type.
	results, err = store.SearchChunks(ctx, "authentication", kernel.SearchOptions{DocType: domain.DocTypeADR})
	if err != nil {
		t.Fatalf("SearchChunks (type filter): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchChunks with DocType filter = %+v, want 1 hit", results)
	}

	// No match.
	results, err = store.SearchChunks(ctx, "nonexistentterm", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("SearchChunks (no match): %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("SearchChunks(nonexistentterm) = %+v, want no hits", results)
	}
}

func TestPutTagsAndPutRelationshipStandalone(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = store.PutRepository(ctx, repo)
	docA := newTestDoc(t, repo.ID, "README.md")
	docA.Tags = nil
	docB := newTestDoc(t, repo.ID, "ARCHITECTURE.md")
	docB.Tags = nil
	if err := store.PutDocument(ctx, docA); err != nil {
		t.Fatalf("PutDocument(docA): %v", err)
	}
	if err := store.PutDocument(ctx, docB); err != nil {
		t.Fatalf("PutDocument(docB): %v", err)
	}

	tag, err := domain.NewTag(docA.ID, "severity", "error")
	if err != nil {
		t.Fatalf("NewTag: %v", err)
	}
	if err := store.PutTags(ctx, docA.ID, []domain.Tag{tag}); err != nil {
		t.Fatalf("PutTags: %v", err)
	}
	tags, err := store.ListTags(ctx, docA.ID)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0].Key != "severity" {
		t.Fatalf("ListTags after PutTags = %+v", tags)
	}

	rel, err := domain.NewRelationship(docA.ID, docB.ID, domain.RelationshipRelated, domain.RelationshipInferred)
	if err != nil {
		t.Fatalf("NewRelationship: %v", err)
	}
	if err := store.PutRelationship(ctx, rel); err != nil {
		t.Fatalf("PutRelationship: %v", err)
	}
	// PutRelationship uses ON CONFLICT DO NOTHING — calling it again
	// with the same (deterministic) id must not error or duplicate.
	if err := store.PutRelationship(ctx, rel); err != nil {
		t.Fatalf("PutRelationship (duplicate): %v", err)
	}
	rels, err := store.ListRelationships(ctx, docA.ID)
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("ListRelationships = %+v, want exactly 1 (duplicate insert ignored)", rels)
	}

	// A document can be the "to" side of the relationship too.
	relsFromB, err := store.ListRelationships(ctx, docB.ID)
	if err != nil {
		t.Fatalf("ListRelationships(docB): %v", err)
	}
	if len(relsFromB) != 1 || relsFromB[0].FromDocumentID != docA.ID {
		t.Fatalf("ListRelationships(docB) = %+v, want to find the edge from docA", relsFromB)
	}
}

func TestIndexStateCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = store.PutRepository(ctx, repo)

	_, ok, err := store.GetIndexState(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetIndexState (missing): %v", err)
	}
	if ok {
		t.Fatal("GetIndexState: expected ok=false before any state is written")
	}

	now := time.Now().UTC().Truncate(time.Second)
	state := kernel.IndexState{
		RepositoryID:    repo.ID,
		DocumentCount:   3,
		LastFullIndexAt: now,
		Status:          kernel.IndexStatusClean,
	}
	if err := store.PutIndexState(ctx, state); err != nil {
		t.Fatalf("PutIndexState: %v", err)
	}

	got, ok, err := store.GetIndexState(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetIndexState: %v", err)
	}
	if !ok {
		t.Fatal("GetIndexState: expected ok=true after PutIndexState")
	}
	if got.DocumentCount != 3 || got.Status != kernel.IndexStatusClean || !got.LastFullIndexAt.Equal(now) {
		t.Fatalf("GetIndexState = %+v, want %+v", got, state)
	}

	// Upsert.
	state.Status = kernel.IndexStatusStale
	state.DocumentCount = 4
	if err := store.PutIndexState(ctx, state); err != nil {
		t.Fatalf("PutIndexState (update): %v", err)
	}
	got, _, err = store.GetIndexState(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetIndexState (after update): %v", err)
	}
	if got.Status != kernel.IndexStatusStale || got.DocumentCount != 4 {
		t.Fatalf("GetIndexState after update = %+v", got)
	}
}
