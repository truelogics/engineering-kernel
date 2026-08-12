package sqlite

import (
	"context"
	"fmt"

	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// SearchChunks implements kernel.Storage: a ranked FTS5 query, optionally
// filtered by repository/doc type. Ranking (bm25) is SQLite's, not a
// judgment call Storage makes on top of it — Search's implementation
// (internal/search) owns any further re-ranking or related-file logic.
func (s *Store) SearchChunks(ctx context.Context, query string, opts kernel.SearchOptions) ([]kernel.ChunkMatch, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	sqlStr := `
		SELECT f.chunk_id, f.document_id, f.heading, snippet(chunks_fts, 0, '**', '**', '...', 12), bm25(chunks_fts)
		FROM chunks_fts f
		JOIN documents d ON d.id = f.document_id
		WHERE chunks_fts MATCH ?`
	args := []any{query}

	if opts.RepositoryID != "" {
		sqlStr += ` AND d.repository_id = ?`
		args = append(args, opts.RepositoryID)
	}
	if opts.DocType != "" {
		sqlStr += ` AND d.doc_type = ?`
		args = append(args, string(opts.DocType))
	}
	sqlStr += ` ORDER BY bm25(chunks_fts) LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: search %q: %w", query, err)
	}
	defer rows.Close()

	type hit struct {
		chunkID, documentID, heading, snippet string
		rank                                  float64
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.chunkID, &h.documentID, &h.heading, &h.snippet, &h.rank); err != nil {
			return nil, fmt.Errorf("sqlite: scan search hit: %w", err)
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	matches := make([]kernel.ChunkMatch, 0, len(hits))
	for _, h := range hits {
		chunk, err := s.getChunk(ctx, h.chunkID)
		if err != nil {
			return nil, err
		}
		doc, err := s.GetDocument(ctx, h.documentID)
		if err != nil {
			return nil, err
		}
		matches = append(matches, kernel.ChunkMatch{
			Chunk:    chunk,
			Document: doc,
			// bm25 is "lower is better" (typically negative); negate
			// so ChunkMatch.Score reads as "higher is more relevant".
			Score:   -h.rank,
			Snippet: h.snippet,
		})
	}
	return matches, nil
}
