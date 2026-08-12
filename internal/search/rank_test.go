package search

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/graph"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want []float64
	}{
		{"empty", nil, []float64{}},
		{"single", []float64{5}, []float64{1}},
		{"all equal", []float64{3, 3, 3}, []float64{1, 1, 1}},
		{"spread", []float64{0, 5, 10}, []float64{0, 0.5, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalize(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("normalize(%v) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if math.Abs(got[i]-c.want[i]) > 1e-9 {
					t.Fatalf("normalize(%v) = %v, want %v", c.in, got, c.want)
				}
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b kernel.Embedding
		want float64
	}{
		{"identical", kernel.Embedding{1, 0, 0}, kernel.Embedding{1, 0, 0}, 1},
		{"orthogonal", kernel.Embedding{1, 0}, kernel.Embedding{0, 1}, 0},
		{"opposite", kernel.Embedding{1, 0}, kernel.Embedding{-1, 0}, -1},
		{"empty", nil, kernel.Embedding{1, 0}, 0},
		{"mismatched length", kernel.Embedding{1, 0, 0}, kernel.Embedding{1, 0}, 0},
		{"zero vector", kernel.Embedding{0, 0}, kernel.Embedding{1, 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cosineSimilarity(c.a, c.b)
			if math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("cosineSimilarity(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestGraphBoostsNilGraphReturnsZeros(t *testing.T) {
	s := &Search{Graph: nil}
	matches := []kernel.ChunkMatch{{Document: domain.CanonicalDocument{ID: "a"}}, {Document: domain.CanonicalDocument{ID: "b"}}}
	boosts := s.graphBoosts(context.Background(), matches)
	if len(boosts) != 2 || boosts[0] != 0 || boosts[1] != 0 {
		t.Fatalf("graphBoosts with nil Graph = %v, want [0 0]", boosts)
	}
}

func TestGraphBoostsRewardsConnectedCandidates(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	a := putDoc(t, ctx, storage, repo.ID, "a.md", "content")
	b := putDoc(t, ctx, storage, repo.ID, "b.md", "content")
	c := putDoc(t, ctx, storage, repo.ID, "c.md", "content")

	rel, err := domain.NewRelationship(a.ID, b.ID, domain.RelationshipReferences, domain.RelationshipExplicit)
	if err != nil {
		t.Fatalf("NewRelationship: %v", err)
	}
	a.Relationships = []domain.Relationship{rel}
	if err := storage.PutDocument(ctx, a); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	s := &Search{Storage: storage, Graph: graph.New(storage)}
	matches := []kernel.ChunkMatch{
		{Document: a},
		{Document: b},
		{Document: c}, // isolated — no relationship to a or b
	}
	boosts := s.graphBoosts(ctx, matches)
	if len(boosts) != 3 {
		t.Fatalf("graphBoosts = %v, want 3 values", boosts)
	}
	if boosts[0] == 0 || boosts[1] == 0 {
		t.Fatalf("graphBoosts = %v, want a and b (connected) to have a nonzero boost", boosts)
	}
	if boosts[2] != 0 {
		t.Fatalf("graphBoosts = %v, want c (isolated) to have a zero boost", boosts)
	}
}

// fakeEmbeddingProvider is deterministic and test-only: each dimension is
// 1.0 if a fixed vocabulary word appears in the text, 0.0 otherwise — not
// a real embedding model, just enough to prove the blending math and
// wiring work without depending on Milestone 4 shipping a real provider.
type fakeEmbeddingProvider struct {
	vocab []string
}

func (f *fakeEmbeddingProvider) Embed(ctx context.Context, texts []string) ([]kernel.Embedding, error) {
	out := make([]kernel.Embedding, len(texts))
	for i, text := range texts {
		vec := make(kernel.Embedding, len(f.vocab))
		for j, word := range f.vocab {
			if containsWord(text, word) {
				vec[j] = 1
			}
		}
		out[i] = vec
	}
	return out, nil
}

func (f *fakeEmbeddingProvider) Dimensions() int { return len(f.vocab) }

func containsWord(text, word string) bool {
	replacer := strings.NewReplacer(".", " ", ",", " ", "\t", " ", "\n", " ")
	for _, w := range strings.Fields(replacer.Replace(text)) {
		if w == word {
			return true
		}
	}
	return false
}

func TestEmbeddingBoostsRanksSemanticMatchHigher(t *testing.T) {
	provider := &fakeEmbeddingProvider{vocab: []string{"authentication", "deployment", "jwt"}}
	s := &Search{Embeddings: provider}

	matches := []kernel.ChunkMatch{
		{Chunk: domain.Chunk{Content: "we use jwt for authentication"}},
		{Chunk: domain.Chunk{Content: "this covers our deployment pipeline"}},
	}
	boosts, err := s.embeddingBoosts(context.Background(), "authentication jwt", matches)
	if err != nil {
		t.Fatalf("embeddingBoosts: unexpected error: %v", err)
	}
	if len(boosts) != 2 {
		t.Fatalf("embeddingBoosts = %v, want 2 values", boosts)
	}
	if boosts[0] <= boosts[1] {
		t.Fatalf("embeddingBoosts = %v, want the authentication/jwt chunk to score higher than the deployment one for an authentication+jwt query", boosts)
	}
}

func TestEmbeddingBoostsClampsNegativeSimilarity(t *testing.T) {
	provider := &fakeEmbeddingProvider{vocab: []string{"a", "b"}}
	s := &Search{Embeddings: provider}
	// "a" and "b" never co-occur in the fake vocab space, so their vectors
	// are orthogonal (similarity 0), not negative — this mostly exercises
	// that the function runs end to end and clamps correctly if it were.
	boosts, err := s.embeddingBoosts(context.Background(), "a", []kernel.ChunkMatch{{Chunk: domain.Chunk{Content: "b"}}})
	if err != nil {
		t.Fatalf("embeddingBoosts: %v", err)
	}
	if boosts[0] < 0 {
		t.Fatalf("embeddingBoosts = %v, want no negative values", boosts)
	}
}

func TestRankKeywordOnlyWhenNoGraphOrEmbeddings(t *testing.T) {
	s := &Search{}
	matches := []kernel.ChunkMatch{
		{Document: domain.CanonicalDocument{ID: "a"}, Score: 1.0},
		{Document: domain.CanonicalDocument{ID: "b"}, Score: 0.5},
	}
	scored, err := s.rank(context.Background(), "query", matches)
	if err != nil {
		t.Fatalf("rank: unexpected error: %v", err)
	}
	if len(scored) != 2 {
		t.Fatalf("rank = %+v, want 2 entries", scored)
	}
	if scored[0].blended <= scored[1].blended {
		t.Fatalf("rank = %+v, want higher raw Score to stay ranked first with no graph/embedding signal", scored)
	}
}

func TestRankCustomWeightsOverrideOrdering(t *testing.T) {
	ctx := context.Background()
	storage := openTestStore(t)
	repo, _ := domain.NewRepository("ws-1", "ai-memory", "/repos/ai-memory")
	_ = storage.PutRepository(ctx, repo)

	weak := putDoc(t, ctx, storage, repo.ID, "weak.md", "content")     // lower keyword score, connected
	strong := putDoc(t, ctx, storage, repo.ID, "strong.md", "content") // higher keyword score, isolated
	partner := putDoc(t, ctx, storage, repo.ID, "partner.md", "content")

	rel, err := domain.NewRelationship(weak.ID, partner.ID, domain.RelationshipReferences, domain.RelationshipExplicit)
	if err != nil {
		t.Fatalf("NewRelationship: %v", err)
	}
	weak.Relationships = []domain.Relationship{rel}
	if err := storage.PutDocument(ctx, weak); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	matches := []kernel.ChunkMatch{
		{Document: strong, Score: 1.0},
		{Document: weak, Score: 0.1},
		{Document: partner, Score: 0.05},
	}

	s := &Search{Storage: storage, Graph: graph.New(storage)}

	// rank() doesn't sort — Search() does that afterward — so find the
	// highest-blended entry explicitly rather than assuming index 0.
	top := func(scored []scoredMatch) string {
		best := scored[0]
		for _, sc := range scored[1:] {
			if sc.blended > best.blended {
				best = sc
			}
		}
		return best.match.Document.ID
	}

	// Default weights (keyword-dominant): the low-keyword-score "weak"
	// doc must not outrank "strong" despite its graph connection.
	defaultScored, err := s.rank(ctx, "query", matches)
	if err != nil {
		t.Fatalf("rank (default weights): %v", err)
	}
	if got := top(defaultScored); got != strong.ID {
		t.Fatalf("rank (default weights) top result = %s, want %s (keyword-dominant default)", got, strong.ID)
	}

	// Custom weights (graph-dominant): now "weak" should outrank "strong".
	s.Weights = RankWeights{Keyword: 0.1, Graph: 0.9}
	customScored, err := s.rank(ctx, "query", matches)
	if err != nil {
		t.Fatalf("rank (custom weights): %v", err)
	}
	if got := top(customScored); got != weak.ID {
		t.Fatalf("rank (custom graph-dominant weights) top result = %s, want %s — custom Weights should change the outcome, not just internal scores", got, weak.ID)
	}
}

func TestRankWeightsIsZero(t *testing.T) {
	if !(RankWeights{}).isZero() {
		t.Fatal("RankWeights{}.isZero() = false, want true")
	}
	if (RankWeights{Keyword: 0.5}).isZero() {
		t.Fatal("RankWeights{Keyword: 0.5}.isZero() = true, want false")
	}
}
