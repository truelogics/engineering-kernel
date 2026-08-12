package sqlite

import (
	"context"
	"fmt"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// PutChunks implements kernel.Storage. Replaces every chunk for
// documentID — Chunker's strategy decides how many chunks and how
// they're split, not Storage — atomically alongside the FTS5 index that
// backs SearchChunks.
func (s *Store) PutChunks(ctx context.Context, documentID string, chunks []domain.Chunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: put chunks for %s: begin tx: %w", documentID, err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM document_chunks WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("sqlite: put chunks for %s: clear: %w", documentID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks_fts WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("sqlite: put chunks for %s: clear fts: %w", documentID, err)
	}

	for _, c := range chunks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO document_chunks (id, document_id, chunk_index, heading, content)
			VALUES (?, ?, ?, ?, ?)
		`, c.ID, documentID, c.Index, c.Heading, c.Content); err != nil {
			return fmt.Errorf("sqlite: put chunks for %s: insert chunk %d: %w", documentID, c.Index, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chunks_fts (content, chunk_id, document_id, heading)
			VALUES (?, ?, ?, ?)
		`, c.Content, c.ID, documentID, c.Heading); err != nil {
			return fmt.Errorf("sqlite: put chunks for %s: index chunk %d: %w", documentID, c.Index, err)
		}
	}

	return tx.Commit()
}

// ListChunks implements kernel.Storage.
func (s *Store) ListChunks(ctx context.Context, documentID string) ([]domain.Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, chunk_index, heading, content
		FROM document_chunks WHERE document_id = ? ORDER BY chunk_index
	`, documentID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list chunks for %s: %w", documentID, err)
	}
	defer rows.Close()

	out := []domain.Chunk{}
	for rows.Next() {
		var c domain.Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.Index, &c.Heading, &c.Content); err != nil {
			return nil, fmt.Errorf("sqlite: scan chunk: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// getChunk fetches a single chunk by id — used by SearchChunks to
// hydrate a full domain.Chunk from an FTS5 match.
func (s *Store) getChunk(ctx context.Context, id string) (domain.Chunk, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, document_id, chunk_index, heading, content FROM document_chunks WHERE id = ?`, id)
	var c domain.Chunk
	if err := row.Scan(&c.ID, &c.DocumentID, &c.Index, &c.Heading, &c.Content); err != nil {
		return domain.Chunk{}, fmt.Errorf("sqlite: get chunk %s: %w", id, err)
	}
	return c, nil
}
