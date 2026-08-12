package search

import (
	"context"
	"errors"
	"math"

	"github.com/truelogics/engineering-kernel/internal/kernel"
)

var errEmbeddingCountMismatch = errors.New("search: embedding provider returned a different number of vectors than texts submitted")

// Default blend weights — used whenever Search.Weights is the zero
// value. Keyword relevance stays the dominant signal by default; graph
// and embedding are refinements, not replacements, per RFC-0003/GRAPH.md's
// framing of hybrid search as making existing results "more accurate,"
// not a second ranking system. Milestone 7 makes these configurable
// rather than fixed — set Search.Weights to override.
const (
	defaultWeightKeywordOnly           = 0.80
	defaultWeightGraphOnly             = 0.20
	defaultWeightKeywordWithEmbeddings = 0.70
	defaultWeightGraphWithEmbeddings   = 0.20
	defaultWeightEmbeddings            = 0.10
)

// RankWeights configures how keyword/graph/embedding signals blend into
// one score (Milestone 7). The zero value means "use Milestone 5's
// defaults" — callers that don't care about tuning ranking never need to
// touch this.
type RankWeights struct {
	Keyword   float64
	Graph     float64
	Embedding float64
}

// isZero reports whether w is the zero value — the "use defaults" signal.
func (w RankWeights) isZero() bool {
	return w == RankWeights{}
}

func defaultWeights(hasEmbeddings bool) RankWeights {
	if hasEmbeddings {
		return RankWeights{Keyword: defaultWeightKeywordWithEmbeddings, Graph: defaultWeightGraphWithEmbeddings, Embedding: defaultWeightEmbeddings}
	}
	return RankWeights{Keyword: defaultWeightKeywordOnly, Graph: defaultWeightGraphOnly}
}

// scoredMatch pairs a raw ChunkMatch with its blended hybrid score.
type scoredMatch struct {
	match   kernel.ChunkMatch
	blended float64
}

// rank blends keyword (BM25, from Storage), graph (connectivity within
// this result set), and — if s.Embeddings is configured — semantic
// similarity into one score per match, using s.Weights (or Milestone 5's
// defaults if unset). Graph and embedding signals are both optional in
// the sense that a zero-value component (no relationships in this pool;
// no EmbeddingProvider configured) degrades to keyword-only ranking, not
// an error.
func (s *Search) rank(ctx context.Context, query string, matches []kernel.ChunkMatch) ([]scoredMatch, error) {
	keyword := normalize(extractScores(matches))
	graphBoost := s.graphBoosts(ctx, matches)

	weights := s.Weights
	if weights.isZero() {
		weights = defaultWeights(s.Embeddings != nil)
	}

	var embedBoost []float64
	if s.Embeddings != nil {
		var err error
		embedBoost, err = s.embeddingBoosts(ctx, query, matches)
		if err != nil {
			return nil, err
		}
	}

	out := make([]scoredMatch, len(matches))
	for i, m := range matches {
		blended := weights.Keyword*keyword[i] + weights.Graph*graphBoost[i]
		if embedBoost != nil {
			blended += weights.Embedding * embedBoost[i]
		}
		out[i] = scoredMatch{match: m, blended: blended}
	}
	return out, nil
}

func extractScores(matches []kernel.ChunkMatch) []float64 {
	out := make([]float64, len(matches))
	for i, m := range matches {
		out[i] = m.Score
	}
	return out
}

// normalize min-max scales values to [0, 1]. If every value is equal
// (including the single-match case), returns all 1.0 rather than
// dividing by zero — one candidate, or a tie, shouldn't be scored 0.
func normalize(values []float64) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 {
		return out
	}
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if max == min {
		for i := range out {
			out[i] = 1.0
		}
		return out
	}
	for i, v := range values {
		out[i] = (v - min) / (max - min)
	}
	return out
}

// graphBoosts scores each match by how many *other* matches in this same
// result set it's connected to (one hop) — a document that shows up
// alongside several other hits it's actually linked to is more likely the
// real answer than one that matched the keywords in isolation. Relies on
// s.Graph; if unset, every boost is 0 (pure keyword ranking).
//
// Sparse by design right now: RFC-0003's Relationships are still rare
// (only `supersedes` and generic path references resolve), so most
// queries will see this contribute nothing — a refinement for when more
// relationships exist, not a signal this milestone claims is
// transformative today.
func (s *Search) graphBoosts(ctx context.Context, matches []kernel.ChunkMatch) []float64 {
	boosts := make([]float64, len(matches))
	if s.Graph == nil || len(matches) < 2 {
		return boosts
	}

	inPool := make(map[string]bool, len(matches))
	for _, m := range matches {
		inPool[m.Document.ID] = true
	}

	maxConnections := 0
	counts := make([]int, len(matches))
	for i, m := range matches {
		neighbors, err := s.Graph.Neighbors(ctx, m.Document.ID)
		if err != nil {
			continue // best-effort: a graph lookup failure degrades to 0 boost, not a search failure
		}
		count := 0
		for _, rel := range neighbors {
			other := rel.ToDocumentID
			if other == m.Document.ID {
				other = rel.FromDocumentID
			}
			if inPool[other] {
				count++
			}
		}
		counts[i] = count
		if count > maxConnections {
			maxConnections = count
		}
	}
	if maxConnections == 0 {
		return boosts
	}
	for i, c := range counts {
		boosts[i] = float64(c) / float64(maxConnections)
	}
	return boosts
}

// embeddingBoosts scores each match by cosine similarity between the
// query and the match's chunk content, using s.Embeddings. Computed on
// the fly (no precomputed/stored embeddings — Storage has no vector
// column yet, per Milestone 4's own note that this waits for a real
// provider to size it against).
func (s *Search) embeddingBoosts(ctx context.Context, query string, matches []kernel.ChunkMatch) ([]float64, error) {
	texts := make([]string, 0, len(matches)+1)
	texts = append(texts, query)
	for _, m := range matches {
		texts = append(texts, m.Chunk.Content)
	}

	vectors, err := s.Embeddings.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(texts) {
		return nil, errEmbeddingCountMismatch
	}

	queryVec := vectors[0]
	sims := make([]float64, len(matches))
	for i, v := range vectors[1:] {
		sims[i] = cosineSimilarity(queryVec, v)
	}
	// Cosine similarity is already in [-1, 1]; clamp negatives to 0 rather
	// than min-max normalizing — a genuinely dissimilar chunk should score
	// near 0, not get rescaled up just because it was the least-similar of
	// this particular pool.
	for i, v := range sims {
		if v < 0 {
			sims[i] = 0
		}
	}
	return sims, nil
}

func cosineSimilarity(a, b kernel.Embedding) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
