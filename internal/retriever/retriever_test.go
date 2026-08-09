package retriever

import (
	"context"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
	"github.com/truelogics/ai-memory/internal/kernel"
	"github.com/truelogics/ai-memory/internal/search"
	"github.com/truelogics/ai-memory/internal/storage/sqlite"
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

func putDoc(t *testing.T, ctx context.Context, storage kernel.Storage, repoID, path, content string, docType domain.DocType) domain.CanonicalDocument {
	t.Helper()
	doc, err := domain.NewCanonicalDocument(repoID, repoID, path)
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Content = content
	doc.Type = docType
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

func TestRetrieveGroupsByDocType(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	putDoc(t, ctx, storage, repo.ID, "ARCHITECTURE.md", "the authentication pipeline architecture", domain.DocTypeStandard)
	putDoc(t, ctx, storage, repo.ID, "adr-0003.md", "authentication decision: use JWT", domain.DocTypeADR)
	putDoc(t, ctx, storage, repo.ID, "rules/auth.md", "authentication must use JWT tokens", domain.DocTypeRule)

	r := New(search.New(storage))
	bundle, err := r.Retrieve(ctx, "authentication", kernel.RetrieveOptions{})
	if err != nil {
		t.Fatalf("Retrieve: unexpected error: %v", err)
	}
	if bundle.Task != "authentication" {
		t.Fatalf("bundle.Task = %q, want %q", bundle.Task, "authentication")
	}

	byLabel := map[string]kernel.RetrievalGroup{}
	for _, g := range bundle.Groups {
		byLabel[g.Label] = g
	}

	if len(byLabel["Architecture"].Results) != 1 {
		t.Errorf("Architecture group = %+v, want 1 result", byLabel["Architecture"])
	}
	if len(byLabel["Related ADRs"].Results) != 1 {
		t.Errorf("Related ADRs group = %+v, want 1 result", byLabel["Related ADRs"])
	}
	if len(byLabel["Rules"].Results) != 1 {
		t.Errorf("Rules group = %+v, want 1 result", byLabel["Rules"])
	}

	// RFC-0001 non-goals: no PR/Issue ingestion — these must be present,
	// but empty, not omitted.
	for _, label := range []string{"Related Issues", "Related PRs"} {
		g, ok := byLabel[label]
		if !ok {
			t.Errorf("expected a %q group to be present (even if empty)", label)
			continue
		}
		if len(g.Results) != 0 {
			t.Errorf("%q group = %+v, want empty (nothing ingests these yet)", label, g)
		}
	}
}

func TestRetrieveNoMatches(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	putDoc(t, ctx, storage, repo.ID, "a.md", "totally unrelated content", domain.DocTypeReadme)

	r := New(search.New(storage))
	bundle, err := r.Retrieve(ctx, "zzzznonexistentterm", kernel.RetrieveOptions{})
	if err != nil {
		t.Fatalf("Retrieve: unexpected error: %v", err)
	}
	for _, g := range bundle.Groups {
		if len(g.Results) != 0 {
			t.Errorf("group %q = %+v, want empty for a query with no matches", g.Label, g)
		}
	}
}

func TestKeywordsDropsStopwordsAndUsesOr(t *testing.T) {
	got := keywords("Review the authentication PR")
	want := `"authentication" OR "pr"`
	if got != want {
		t.Fatalf("keywords(...) = %q, want %q", got, want)
	}
}

func TestKeywordsFallsBackToRawTaskWhenAllStopwords(t *testing.T) {
	got := keywords("the a an")
	if got != "the a an" {
		t.Fatalf("keywords(all stopwords) = %q, want the original task unchanged", got)
	}
}

// TestKeywordsQuotesTermsWithFTS5SpecialCharacters guards against a real
// bug a consumer (ai-review's deterministic TaskBuilder, RFC-0002) hit:
// a task like "Modified: README.md" produced bareword terms "modified:"
// and "readme.md" — FTS5 parses a trailing colon as its column-filter
// syntax ("column:term", crashing with "no such column: modified"), and
// an embedded period breaks bareword query parsing entirely ("syntax
// error near '.'"). Every term must come back quoted so FTS5 treats it
// as a literal phrase instead of parsing its punctuation as syntax.
func TestKeywordsQuotesTermsWithFTS5SpecialCharacters(t *testing.T) {
	got := keywords("Modified: README.md")
	want := `"modified" OR "readme.md"`
	if got != want {
		t.Fatalf("keywords(%q) = %q, want %q", "Modified: README.md", got, want)
	}
}

func TestRetrieveDefaultPriorityOrder(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	putDoc(t, ctx, storage, repo.ID, "adr.md", "authentication", domain.DocTypeADR)
	putDoc(t, ctx, storage, repo.ID, "arch.md", "authentication", domain.DocTypeStandard)

	r := New(search.New(storage))
	bundle, err := r.Retrieve(ctx, "authentication", kernel.RetrieveOptions{})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(bundle.Groups) < 2 || bundle.Groups[0].Label != "Architecture" || bundle.Groups[1].Label != "Related ADRs" {
		labels := make([]string, len(bundle.Groups))
		for i, g := range bundle.Groups {
			labels[i] = g.Label
		}
		t.Fatalf("group order = %v, want Architecture before Related ADRs (default priority)", labels)
	}
}

func TestRetrieveCustomPriorityReordersGroups(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	putDoc(t, ctx, storage, repo.ID, "adr.md", "authentication", domain.DocTypeADR)
	putDoc(t, ctx, storage, repo.ID, "arch.md", "authentication", domain.DocTypeStandard)
	putDoc(t, ctx, storage, repo.ID, "rule.md", "authentication", domain.DocTypeRule)

	r := &Retriever{Search: search.New(storage), Priority: []string{"Rules", "Related ADRs"}}
	bundle, err := r.Retrieve(ctx, "authentication", kernel.RetrieveOptions{})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	var labels []string
	for _, g := range bundle.Groups {
		labels = append(labels, g.Label)
	}
	if len(labels) < 2 || labels[0] != "Rules" || labels[1] != "Related ADRs" {
		t.Fatalf("group order = %v, want custom Priority [Rules, Related ADRs] to come first", labels)
	}
	// Architecture wasn't in the custom Priority — it must still appear
	// (afterward), not be silently dropped.
	found := false
	for _, l := range labels {
		if l == "Architecture" {
			found = true
		}
	}
	if !found {
		t.Fatalf("group order = %v, want Architecture still present even though the custom Priority didn't mention it", labels)
	}
}

// putScopedRule stores a rule document declaring an applies_to scope.
func putScopedRule(t *testing.T, ctx context.Context, storage kernel.Storage, repoID, path, content, appliesTo string) {
	t.Helper()
	doc, err := domain.NewCanonicalDocument(repoID, repoID, path)
	if err != nil {
		t.Fatalf("NewCanonicalDocument: %v", err)
	}
	doc.Content = content
	doc.Type = domain.DocTypeRule
	doc.Metadata = domain.NewMetadata()
	if appliesTo != "" {
		doc.Metadata.Set(domain.AppliesToKey, appliesTo)
	}
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
}

func rulePaths(bundle kernel.RetrievalBundle) []string {
	var out []string
	for _, g := range bundle.Groups {
		if g.Label != "Rules" {
			continue
		}
		for _, r := range g.Results {
			out = append(out, r.Document.Path)
		}
	}
	return out
}

// TestRetrieveScopesRulesToChangedPaths is RFC-0005's motivating case:
// the first cross-repository retrieval this kernel ever performed
// returned rules/ts-no-floating-promises.md for a diff touching only Go
// files, because nothing compared a rule's declared scope against the
// files under review.
func TestRetrieveScopesRulesToChangedPaths(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	body := "Errors and promises must be handled, never dropped or floated."
	putScopedRule(t, ctx, storage, repo.ID, "rules/ts-no-floating-promises.md", body, "**/*.ts, **/*.tsx")
	putScopedRule(t, ctx, storage, repo.ID, "rules/go-wrap-errors.md", body, "go")
	putScopedRule(t, ctx, storage, repo.ID, "rules/pr-single-purpose.md", body, "")

	r := New(&search.Search{Storage: storage})
	bundle, err := r.Retrieve(ctx, "errors promises handled dropped",
		kernel.RetrieveOptions{ChangedPaths: []string{"internal/provider/claude/claude.go"}})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	got := rulePaths(bundle)
	for _, p := range got {
		if p == "rules/ts-no-floating-promises.md" {
			t.Errorf("a TypeScript rule was returned for a Go-only change: %v", got)
		}
	}
	var sawGo, sawUniversal bool
	for _, p := range got {
		sawGo = sawGo || p == "rules/go-wrap-errors.md"
		sawUniversal = sawUniversal || p == "rules/pr-single-purpose.md"
	}
	if !sawGo {
		t.Errorf("the Go rule was not returned for a Go change: %v", got)
	}
	if !sawUniversal {
		t.Errorf("an unscoped rule must stay universal: %v", got)
	}
}

// TestRetrieveWithoutPathsAppliesNoScoping pins the backward-compatible
// path: `eng ask` supplies no changed paths and must behave as before.
func TestRetrieveWithoutPathsAppliesNoScoping(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	body := "Errors and promises must be handled, never dropped or floated."
	putScopedRule(t, ctx, storage, repo.ID, "rules/ts-no-floating-promises.md", body, "**/*.ts")

	r := New(&search.Search{Storage: storage})
	bundle, err := r.Retrieve(ctx, "errors promises handled dropped", kernel.RetrieveOptions{})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(rulePaths(bundle)) != 1 {
		t.Errorf("with no changed paths every rule should be returned, got %v", rulePaths(bundle))
	}
}

// TestRetrieveReturnsNoRulesWhenNoneGovern covers RFC-0005's third
// fallback: an empty Rules group is the honest answer, not a reason to
// fall back to unfiltered results.
func TestRetrieveReturnsNoRulesWhenNoneGovern(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)
	body := "Errors and promises must be handled, never dropped or floated."
	putScopedRule(t, ctx, storage, repo.ID, "rules/ts-no-floating-promises.md", body, "**/*.ts")

	r := New(&search.Search{Storage: storage})
	bundle, err := r.Retrieve(ctx, "errors promises handled dropped",
		kernel.RetrieveOptions{ChangedPaths: []string{"main.go"}})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got := rulePaths(bundle); len(got) != 0 {
		t.Errorf("expected no rules to govern a Go change, got %v", got)
	}
}
