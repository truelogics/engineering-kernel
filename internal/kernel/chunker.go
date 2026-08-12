package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// Chunker splits a CanonicalDocument into Chunks — the unit Search
// actually indexes, so a query returns a matched section instead of "the
// whole file matched." Owns the chunking strategy (heading, paragraph,
// fixed-size — see internal/chunker).
type Chunker interface {
	// Chunk must not persist chunks (Indexer/Storage's job) or rank
	// them (Search's job).
	Chunk(ctx context.Context, doc domain.CanonicalDocument) ([]domain.Chunk, error)
}
