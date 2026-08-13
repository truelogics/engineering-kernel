package search

import (
	"context"
	"strings"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
	"github.com/truelogics/engineering-kernel/internal/storage/sqlite"
)

func openTestStore(t *testing.T) kernel.Storage {
	t.Helper()
	store, err := sqlite.Open("file:" + t.Name() + "?mode=memory&cache=private")
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func putDoc(t *testing.T, ctx context.Context, storage kernel.Storage, repoID, path, content string, tags ...domain.Tag) domain.CanonicalDocument {
	t.Helper()
	doc, err := domain.NewCanonicalDocument(repoID, repoID, path)
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Content = content
	doc.Tags = tags
	if err := storage.PutDocument(ctx, doc); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	chunk, err := domain.NewChunk(doc.ID, 0, "", content)
	if err != nil {
		t.Fatalf("NewChunk: %v", err)
	}
	if err := storage.PutChunks(ctx, doc.ID, []domain.Chunk{chunk}); err != nil {
		t.Fatalf("PutChunks: %v", err)
	}
	return doc
}

func TestSearchReturnsRankedResults(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	if err := storage.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	putDoc(t, ctx, storage, repo.ID, "auth.md", "we chose JWT for stateless authentication")
	putDoc(t, ctx, storage, repo.ID, "deploy.md", "this covers our deployment pipeline")

	results, err := New(storage).Search(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Document.Path != "auth.md" {
		t.Fatalf("Search(authentication) = %+v, want 1 hit on auth.md", results)
	}
	if results[0].Snippet == "" {
		t.Error("expected a non-empty snippet")
	}
}

func TestSearchRelatedByExplicitRelationship(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	docADR := putDoc(t, ctx, storage, repo.ID, "adr-0003.md", "authentication decision record")
	docArch := putDoc(t, ctx, storage, repo.ID, "ARCHITECTURE.md", "some unrelated architecture text about deployment")

	rel, err := domain.NewRelationship(docADR.ID, docArch.ID, domain.RelationshipReferences, domain.RelationshipExplicit)
	if err != nil {
		t.Fatalf("NewRelationship: %v", err)
	}
	if err := storage.PutRelationship(ctx, rel); err != nil {
		t.Fatalf("PutRelationship: %v", err)
	}

	results, err := New(storage).Search(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search = %+v, want 1 hit", results)
	}
	if len(results[0].Related) != 1 || results[0].Related[0].ID != docArch.ID {
		t.Fatalf("Related = %+v, want 1 related doc (docArch) via explicit relationship", results[0].Related)
	}
}

func TestSearchRelatedBySharedTagFallback(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	docA := putDoc(t, ctx, storage, repo.ID, "a.md", "we chose JWT for authentication")
	docB := putDoc(t, ctx, storage, repo.ID, "b.md", "totally different content here")

	tagA, _ := domain.NewTag(docA.ID, "audience", "agent")
	tagB, _ := domain.NewTag(docB.ID, "audience", "agent")
	if err := storage.PutTags(ctx, docA.ID, []domain.Tag{tagA}); err != nil {
		t.Fatalf("PutTags(A): %v", err)
	}
	if err := storage.PutTags(ctx, docB.ID, []domain.Tag{tagB}); err != nil {
		t.Fatalf("PutTags(B): %v", err)
	}

	results, err := New(storage).Search(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search = %+v, want 1 hit", results)
	}
	if len(results[0].Related) != 1 || results[0].Related[0].ID != docB.ID {
		t.Fatalf("Related = %+v, want docB via shared 'audience' tag", results[0].Related)
	}
}

func TestSearchRelatedIgnoresStructuralStatsTags(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	docA := putDoc(t, ctx, storage, repo.ID, "a.md", "we chose JWT for authentication")
	docB := putDoc(t, ctx, storage, repo.ID, "b.md", "totally unrelated content")

	tagA, _ := domain.NewTag(docA.ID, "heading_count", "3")
	tagB, _ := domain.NewTag(docB.ID, "heading_count", "3")
	_ = storage.PutTags(ctx, docA.ID, []domain.Tag{tagA})
	_ = storage.PutTags(ctx, docB.ID, []domain.Tag{tagB})

	results, err := New(storage).Search(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search = %+v, want 1 hit", results)
	}
	if len(results[0].Related) != 0 {
		t.Fatalf("Related = %+v, want none — heading_count is a structural stat, not a meaningful shared tag", results[0].Related)
	}
}

func TestSearchNoMatchReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	putDoc(t, ctx, storage, repo.ID, "a.md", "some content")

	results, err := New(storage).Search(ctx, "nonexistentterm", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search(nonexistentterm) = %+v, want no results", results)
	}
}

// putDocWithChunks is the real shape of a long document: one document,
// several chunks, each of which FTS can match independently. putDoc
// stores exactly one chunk and so cannot reproduce this.
func putDocWithChunks(t *testing.T, ctx context.Context, storage kernel.Storage, repoID, path string, bodies ...string) domain.CanonicalDocument {
	t.Helper()
	doc, err := domain.NewCanonicalDocument(repoID, repoID, path)
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Content = strings.Join(bodies, "\n\n")
	if err := storage.PutDocument(ctx, doc); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	chunks := make([]domain.Chunk, 0, len(bodies))
	for i, body := range bodies {
		chunk, err := domain.NewChunk(doc.ID, i, "", body)
		if err != nil {
			t.Fatalf("NewChunk: %v", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := storage.PutChunks(ctx, doc.ID, chunks); err != nil {
		t.Fatalf("PutChunks: %v", err)
	}
	return doc
}

// A long document matches on several chunks. Returning it once per
// matching chunk is not only untidy — the repeats consume result slots,
// so genuinely different documents fall off the end of the limit.
//
// engineering-mcp's /review-branch command already tells the model that
// "only the top-scoring passage per document is kept", so this is the
// documented contract, not a change of behaviour.
func TestSearchReturnsOneRowPerDocument(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "pivot", "/repos/pivot")
	if err := storage.PutRepository(ctx, repo); err != nil {
		t.Fatalf("PutRepository: %v", err)
	}

	putDocWithChunks(t, ctx, storage, repo.ID, "handbook/live-audio.md",
		"live audio and video overview",
		"live audio routing details",
		"live audio device selection",
		"live audio troubleshooting")
	putDoc(t, ctx, storage, repo.ID, "plans/streaming-room.md", "live audio rooms plan")
	putDoc(t, ctx, storage, repo.ID, "plans/captions.md", "live audio captions")

	// A limit smaller than the number of matching chunks: without
	// collapsing, one document's four chunks fill it and the other two
	// documents are never returned.
	results, err := New(storage).Search(ctx, "live audio", kernel.SearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	seen := map[string]int{}
	for _, r := range results {
		seen[r.Document.Path]++
	}
	for path, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times, want 1", path, n)
		}
	}
	if len(seen) != 3 {
		t.Errorf("covered %d distinct documents, want 3 — repeats crowded them out: %v", len(seen), seen)
	}
}

// Collapsing must not reorder: a document's rank is decided by its best
// passage, and matching twice must not move it.
func TestCollapseKeepsBestScoreAndOrder(t *testing.T) {
	in := []scoredMatch{
		{blended: 0.9, match: kernel.ChunkMatch{Document: domain.CanonicalDocument{ID: "a", Path: "a.md"}}},
		{blended: 0.8, match: kernel.ChunkMatch{Document: domain.CanonicalDocument{ID: "b", Path: "b.md"}}},
		{blended: 0.7, match: kernel.ChunkMatch{Document: domain.CanonicalDocument{ID: "a", Path: "a.md"}}},
		{blended: 0.6, match: kernel.ChunkMatch{Document: domain.CanonicalDocument{ID: "c", Path: "c.md"}}},
	}
	out := collapseToDocuments(in)

	if len(out) != 3 {
		t.Fatalf("got %d rows, want 3", len(out))
	}
	wantIDs := []string{"a", "b", "c"}
	wantScores := []float64{0.9, 0.8, 0.6}
	for i := range out {
		if out[i].match.Document.ID != wantIDs[i] || out[i].blended != wantScores[i] {
			t.Errorf("row %d = %s/%.1f, want %s/%.1f",
				i, out[i].match.Document.ID, out[i].blended, wantIDs[i], wantScores[i])
		}
	}
}
