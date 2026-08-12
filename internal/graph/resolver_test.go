package graph

import (
	"context"
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

func putDoc(t *testing.T, ctx context.Context, storage kernel.Storage, repoID, path string, docType domain.DocType) domain.CanonicalDocument {
	t.Helper()
	doc, err := domain.NewCanonicalDocument(repoID, repoID, path)
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Type = docType
	if err := storage.PutDocument(ctx, doc); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	return doc
}

func TestResolvePathLikeReference(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	target := putDoc(t, ctx, storage, repo.ID, "docs/architecture/ARCHITECTURE.md", domain.DocTypeStandard)

	resolver := NewResolver(storage)
	got, ok, err := resolver.Resolve(ctx, kernel.Reference{
		RepositoryID: repo.ID,
		DocType:      domain.DocTypeReadme,
		Value:        "./docs/architecture/ARCHITECTURE.md",
	})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if !ok || got != target.ID {
		t.Fatalf("Resolve(path) = (%q, %v), want (%q, true)", got, ok, target.ID)
	}
}

func TestResolveNumericReferenceIsNamespaceAware(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	rfc := putDoc(t, ctx, storage, repo.ID, "rfcs/0001-engineering-memory-kernel.md", domain.DocTypeRFC)
	// An unrelated ADR that happens to share the number "0001" — must
	// NOT be matched when resolving from an RFC's perspective, since
	// resolution is namespace- (doc type-) scoped, not just numeric.
	putDoc(t, ctx, storage, repo.ID, "engineering/ADR/0001-unrelated-decision.md", domain.DocTypeADR)

	resolver := NewResolver(storage)
	got, ok, err := resolver.Resolve(ctx, kernel.Reference{
		RepositoryID: repo.ID,
		DocType:      domain.DocTypeRFC,
		Value:        "0001",
	})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if !ok || got != rfc.ID {
		t.Fatalf("Resolve(0001) = (%q, %v), want the RFC (%q, true), not the same-numbered ADR", got, ok, rfc.ID)
	}
}

func TestResolveIsNotFilesystemDirectoryScoped(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	// The target lives in a completely different directory than any
	// referencing document would — resolution must still find it, since
	// it's scoped by repository + doc type, not by directory.
	target := putDoc(t, ctx, storage, repo.ID, "some/deeply/nested/dir/0007-decision.md", domain.DocTypeADR)

	resolver := NewResolver(storage)
	got, ok, err := resolver.Resolve(ctx, kernel.Reference{
		RepositoryID: repo.ID,
		DocType:      domain.DocTypeADR,
		Value:        "0007",
	})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if !ok || got != target.ID {
		t.Fatalf("Resolve(0007) = (%q, %v), want %q regardless of directory", got, ok, target.ID)
	}
}

func TestResolveUnresolvableReturnsNotOK(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	resolver := NewResolver(storage)
	_, ok, err := resolver.Resolve(ctx, kernel.Reference{
		RepositoryID: repo.ID,
		DocType:      domain.DocTypeRFC,
		Value:        "9999",
	})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if ok {
		t.Fatal("Resolve: expected ok=false for a reference with no matching document")
	}
}

func TestResolveEmptyValue(t *testing.T) {
	resolver := NewResolver(nil) // Storage never called for an empty value
	_, ok, err := resolver.Resolve(context.Background(), kernel.Reference{Value: "   "})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if ok {
		t.Fatal("Resolve: expected ok=false for an empty reference value")
	}
}

func TestNumericPrefixMatches(t *testing.T) {
	cases := []struct {
		path, number string
		want         bool
	}{
		{"rfcs/0001-engineering-memory-kernel.md", "0001", true},
		{"0001.md", "0001", true},
		{"rfcs/00010-not-a-match.md", "0001", false},
		{"rfcs/0002-knowledge-engine.md", "0001", false},
	}
	for _, c := range cases {
		if got := numericPrefixMatches(c.path, c.number); got != c.want {
			t.Errorf("numericPrefixMatches(%q, %q) = %v, want %v", c.path, c.number, got, c.want)
		}
	}
}

func TestLooksLikePath(t *testing.T) {
	cases := map[string]bool{
		"docs/architecture/X.md": true,
		"X.md":                   true,
		"0001":                   false,
		"":                       false,
		"https://example.com":    false,
	}
	for v, want := range cases {
		if got := looksLikePath(v); got != want {
			t.Errorf("looksLikePath(%q) = %v, want %v", v, got, want)
		}
	}
}
