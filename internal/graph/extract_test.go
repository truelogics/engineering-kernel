package graph

import (
	"context"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

func TestExtractSupersedes(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	old := putDoc(t, ctx, storage, repo.ID, "rfcs/0001-old.md", domain.DocTypeRFC)

	doc, err := domain.NewCanonicalDocument(repo.ID, repo.ID, "rfcs/0002-new.md")
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Type = domain.DocTypeRFC
	doc.Metadata.Set("supersedes", "0001")

	rels, err := Extract(ctx, doc, NewResolver(storage))
	if err != nil {
		t.Fatalf("Extract: unexpected error: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("Extract = %+v, want 1 relationship", rels)
	}
	if rels[0].FromDocumentID != doc.ID || rels[0].ToDocumentID != old.ID {
		t.Fatalf("Extract relationship = %+v, want from=%s to=%s", rels[0], doc.ID, old.ID)
	}
	if rels[0].Type != domain.RelationshipSupersedes {
		t.Fatalf("Extract relationship Type = %q, want %q", rels[0].Type, domain.RelationshipSupersedes)
	}
}

func TestExtractGenericPathReference(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	arch := putDoc(t, ctx, storage, repo.ID, "docs/architecture/ARCHITECTURE.md", domain.DocTypeStandard)

	doc, err := domain.NewCanonicalDocument(repo.ID, repo.ID, "README.md")
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Type = domain.DocTypeReadme
	doc.Metadata.Set("related", "docs/architecture/ARCHITECTURE.md")

	rels, err := Extract(ctx, doc, NewResolver(storage))
	if err != nil {
		t.Fatalf("Extract: unexpected error: %v", err)
	}
	if len(rels) != 1 || rels[0].ToDocumentID != arch.ID || rels[0].Type != domain.RelationshipReferences {
		t.Fatalf("Extract = %+v, want 1 references relationship to %s", rels, arch.ID)
	}
}

func TestExtractIgnoresSupersededByAndResultingADR(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	putDoc(t, ctx, storage, repo.ID, "rfcs/0002-newer.md", domain.DocTypeRFC)

	doc, err := domain.NewCanonicalDocument(repo.ID, repo.ID, "rfcs/0001-old.md")
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Type = domain.DocTypeRFC
	doc.Metadata.Set("superseded_by", "0002")
	doc.Metadata.Set("resulting_adr", "0005")

	rels, err := Extract(ctx, doc, NewResolver(storage))
	if err != nil {
		t.Fatalf("Extract: unexpected error: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("Extract = %+v, want none — superseded_by/resulting_adr are deliberately excluded", rels)
	}
}

func TestExtractSkipsUnresolvableAndSelfReference(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	doc, err := domain.NewCanonicalDocument(repo.ID, repo.ID, "rfcs/0001-self.md")
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Type = domain.DocTypeRFC
	doc.Metadata.Set("supersedes", "9999")           // unresolvable
	doc.Metadata.Set("related", "rfcs/0001-self.md") // resolves to itself

	rels, err := Extract(ctx, doc, NewResolver(storage))
	if err != nil {
		t.Fatalf("Extract: unexpected error: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("Extract = %+v, want none (unresolvable + self-reference both skipped)", rels)
	}
}

func TestExtractDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	putDoc(t, ctx, storage, repo.ID, "a.md", domain.DocTypeReadme)
	putDoc(t, ctx, storage, repo.ID, "b.md", domain.DocTypeReadme)

	doc, err := domain.NewCanonicalDocument(repo.ID, repo.ID, "c.md")
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Type = domain.DocTypeReadme
	doc.Metadata.Set("z_ref", "a.md")
	doc.Metadata.Set("a_ref", "b.md")

	resolver := NewResolver(storage)
	first, err := Extract(ctx, doc, resolver)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	second, err := Extract(ctx, doc, resolver)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("Extract lengths = %d, %d, want 2 both times", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("Extract order not deterministic: run1=%+v run2=%+v", first, second)
		}
	}
}
