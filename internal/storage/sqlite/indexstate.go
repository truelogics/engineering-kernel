package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// GetIndexState implements kernel.Storage.
func (s *Store) GetIndexState(ctx context.Context, repositoryID string) (kernel.IndexState, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT repository_id, document_count, last_full_index_at, last_incremental_index_at, status
		FROM index_state WHERE repository_id = ?
	`, repositoryID)

	var st kernel.IndexState
	var status string
	var lastFull, lastIncr sql.NullTime
	if err := row.Scan(&st.RepositoryID, &st.DocumentCount, &lastFull, &lastIncr, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return kernel.IndexState{}, false, nil
		}
		return kernel.IndexState{}, false, fmt.Errorf("sqlite: get index state for %s: %w", repositoryID, err)
	}
	st.LastFullIndexAt = scanTime(lastFull)
	st.LastIncrementalIndexAt = scanTime(lastIncr)
	st.Status = kernel.IndexStatus(status)
	return st, true, nil
}

// PutIndexState implements kernel.Storage.
func (s *Store) PutIndexState(ctx context.Context, state kernel.IndexState) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO index_state (repository_id, document_count, last_full_index_at, last_incremental_index_at, status)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repository_id) DO UPDATE SET
			document_count=excluded.document_count,
			last_full_index_at=excluded.last_full_index_at,
			last_incremental_index_at=excluded.last_incremental_index_at,
			status=excluded.status
	`, state.RepositoryID, state.DocumentCount, nullTime(state.LastFullIndexAt), nullTime(state.LastIncrementalIndexAt), string(state.Status))
	if err != nil {
		return fmt.Errorf("sqlite: put index state for %s: %w", state.RepositoryID, err)
	}
	return nil
}
