package sqlite

import (
	"context"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

func TestTraverseRelationshipsBoundedByDepth(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = store.PutRepository(ctx, repo)

	// Chain: A -> B -> C -> D
	docs := make([]domain.CanonicalDocument, 4)
	for i, name := range []string{"a.md", "b.md", "c.md", "d.md"} {
		docs[i] = newTestDoc(t, repo.ID, name)
		docs[i].Tags = nil
	}
	for i := 0; i < 3; i++ {
		rel, err := domain.NewRelationship(docs[i].ID, docs[i+1].ID, domain.RelationshipReferences, domain.RelationshipExplicit)
		if err != nil {
			t.Fatalf("NewRelationship: %v", err)
		}
		docs[i].Relationships = []domain.Relationship{rel}
	}
	for _, d := range docs {
		if err := store.PutDocument(ctx, d); err != nil {
			t.Fatalf("PutDocument(%s): %v", d.Path, err)
		}
	}

	oneHop, err := store.TraverseRelationships(ctx, docs[0].ID, 1)
	if err != nil {
		t.Fatalf("TraverseRelationships(depth=1): %v", err)
	}
	if len(oneHop) != 1 || oneHop[0] != docs[1].ID {
		t.Fatalf("TraverseRelationships(depth=1) = %v, want just docs[1]", oneHop)
	}

	threeHops, err := store.TraverseRelationships(ctx, docs[0].ID, 3)
	if err != nil {
		t.Fatalf("TraverseRelationships(depth=3): %v", err)
	}
	if len(threeHops) != 3 {
		t.Fatalf("TraverseRelationships(depth=3) = %v, want all 3 reachable docs", threeHops)
	}

	none, err := store.TraverseRelationships(ctx, docs[0].ID, 0)
	if err != nil {
		t.Fatalf("TraverseRelationships(depth=0): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("TraverseRelationships(depth=0) = %v, want none", none)
	}
}
