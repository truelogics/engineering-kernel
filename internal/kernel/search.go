package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// SearchResult is one ranked hit — what `eng search` prints a row for.
type SearchResult struct {
	Document domain.CanonicalDocument
	Chunk    domain.Chunk
	Score    float64
	Snippet  string
	Related  []domain.CanonicalDocument
}

// Search retrieves matching knowledge — a ranked full-text query over
// Storage. Must not write to Storage, or assemble multiple results into a
// labeled bundle for a task (Retriever's job).
type Search interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
}
