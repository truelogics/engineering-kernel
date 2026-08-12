package graph

import (
	"context"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

func TestNeighborsIsOneHop(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	a := putDoc(t, ctx, storage, repo.ID, "a.md", domain.DocTypeReadme)
	b := putDoc(t, ctx, storage, repo.ID, "b.md", domain.DocTypeReadme)
	rel, err := domain.NewRelationship(a.ID, b.ID, domain.RelationshipReferences, domain.RelationshipExplicit)
	if err != nil {
		t.Fatalf("NewRelationship: %v", err)
	}
	a.Relationships = []domain.Relationship{rel}
	if err := storage.PutDocument(ctx, a); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	rels, err := New(storage).Neighbors(ctx, a.ID)
	if err != nil {
		t.Fatalf("Neighbors: unexpected error: %v", err)
	}
	if len(rels) != 1 || rels[0].ToDocumentID != b.ID {
		t.Fatalf("Neighbors = %+v, want 1 edge to %s", rels, b.ID)
	}
}

func TestTraverseReturnsOnlyEdgesWithinReachableSet(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	// A -> B -> C -> D (chain of 4)
	docs := make([]domain.CanonicalDocument, 4)
	for i, name := range []string{"a.md", "b.md", "c.md", "d.md"} {
		doc, err := domain.NewCanonicalDocument(repo.ID, repo.ID, name)
		if err != nil {
			t.Fatalf("NewCanonicalDocument: %v", err)
		}
		docs[i] = doc
	}
	for i := 0; i < 3; i++ {
		rel, err := domain.NewRelationship(docs[i].ID, docs[i+1].ID, domain.RelationshipReferences, domain.RelationshipExplicit)
		if err != nil {
			t.Fatalf("NewRelationship: %v", err)
		}
		docs[i].Relationships = []domain.Relationship{rel}
	}
	for _, d := range docs {
		if err := storage.PutDocument(ctx, d); err != nil {
			t.Fatalf("PutDocument(%s): %v", d.Path, err)
		}
	}

	sub, err := New(storage).Traverse(ctx, docs[0].ID, 2)
	if err != nil {
		t.Fatalf("Traverse: unexpected error: %v", err)
	}
	if sub.Root != docs[0].ID {
		t.Fatalf("Traverse Root = %q, want %q", sub.Root, docs[0].ID)
	}
	// depth=2 reaches A, B, C — the edge C->D must NOT appear (D is one
	// hop beyond depth, even though C is reachable).
	if len(sub.Edges) != 2 {
		t.Fatalf("Traverse(depth=2).Edges = %+v, want exactly 2 (A->B, B->C), not C->D", sub.Edges)
	}
	for _, e := range sub.Edges {
		if e.ToDocumentID == docs[3].ID || e.FromDocumentID == docs[3].ID {
			t.Fatalf("Traverse(depth=2) included an edge touching docs[3], which is beyond depth: %+v", e)
		}
	}
}

func TestTraverseNoRelationships(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	doc := putDoc(t, ctx, storage, repo.ID, "lonely.md", domain.DocTypeReadme)

	sub, err := New(storage).Traverse(ctx, doc.ID, 2)
	if err != nil {
		t.Fatalf("Traverse: unexpected error: %v", err)
	}
	if len(sub.Edges) != 0 {
		t.Fatalf("Traverse of an isolated document = %+v, want no edges", sub.Edges)
	}
}
