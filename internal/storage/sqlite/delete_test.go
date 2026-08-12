package sqlite

import (
	"context"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

func TestDeleteDocumentRemovesEverything(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = store.PutRepository(ctx, repo)

	docA := newTestDoc(t, repo.ID, "a.md")
	docB := newTestDoc(t, repo.ID, "b.md")
	rel, _ := domain.NewRelationship(docA.ID, docB.ID, domain.RelationshipReferences, domain.RelationshipExplicit)
	docA.Relationships = []domain.Relationship{rel}
	if err := store.PutDocument(ctx, docB); err != nil {
		t.Fatalf("PutDocument(docB): %v", err)
	}
	if err := store.PutDocument(ctx, docA); err != nil {
		t.Fatalf("PutDocument(docA): %v", err)
	}
	chunk, _ := domain.NewChunk(docA.ID, 0, "", "authentication content")
	if err := store.PutChunks(ctx, docA.ID, []domain.Chunk{chunk}); err != nil {
		t.Fatalf("PutChunks: %v", err)
	}

	if err := store.DeleteDocument(ctx, docA.ID); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	if _, err := store.GetDocument(ctx, docA.ID); err == nil {
		t.Fatal("GetDocument: expected error, document should be deleted")
	}
	chunks, err := store.ListChunks(ctx, docA.ID)
	if err != nil {
		t.Fatalf("ListChunks: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("ListChunks after delete = %+v, want none", chunks)
	}
	tags, err := store.ListTags(ctx, docA.ID)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("ListTags after delete = %+v, want none", tags)
	}
	rels, err := store.ListRelationships(ctx, docA.ID)
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("ListRelationships after delete = %+v, want none (edge touching deleted doc gone from both sides)", rels)
	}
	results, err := store.SearchChunks(ctx, "authentication", kernel.SearchOptions{})
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("SearchChunks after delete = %+v, want no hits (FTS row must be gone too)", results)
	}

	// docB (the other endpoint of the deleted relationship) must be
	// unaffected.
	if _, err := store.GetDocument(ctx, docB.ID); err != nil {
		t.Fatalf("GetDocument(docB) after deleting docA: %v", err)
	}
}
