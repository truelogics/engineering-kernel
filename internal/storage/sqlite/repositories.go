package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/truelogics/ai-memory/internal/domain"
)

const repositoryColumns = `id, workspace_id, name, remote_url, local_path, last_indexed_commit, last_indexed_at`

func scanRepository(sc scanner) (domain.Repository, error) {
	var r domain.Repository
	var lastIndexedAt sql.NullTime
	if err := sc.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.RemoteURL, &r.LocalPath, &r.LastIndexedCommit, &lastIndexedAt); err != nil {
		return domain.Repository{}, err
	}
	r.LastIndexedAt = scanTime(lastIndexedAt)
	return r, nil
}

// PutRepository implements kernel.Storage.
func (s *Store) PutRepository(ctx context.Context, repo domain.Repository) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repositories (`+repositoryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id=excluded.workspace_id,
			name=excluded.name,
			remote_url=excluded.remote_url,
			local_path=excluded.local_path,
			last_indexed_commit=excluded.last_indexed_commit,
			last_indexed_at=excluded.last_indexed_at
	`, repo.ID, repo.WorkspaceID, repo.Name, repo.RemoteURL, repo.LocalPath, repo.LastIndexedCommit, nullTime(repo.LastIndexedAt))
	if err != nil {
		return fmt.Errorf("sqlite: put repository %s: %w", repo.ID, err)
	}
	return nil
}

// GetRepository implements kernel.Storage.
func (s *Store) GetRepository(ctx context.Context, id string) (domain.Repository, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+repositoryColumns+` FROM repositories WHERE id = ?`, id)
	repo, err := scanRepository(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("sqlite: repository %s not found", id)
		}
		return domain.Repository{}, fmt.Errorf("sqlite: get repository %s: %w", id, err)
	}
	return repo, nil
}

// FindRepositoryByPath implements kernel.Storage.
func (s *Store) FindRepositoryByPath(ctx context.Context, localPath string) (domain.Repository, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+repositoryColumns+` FROM repositories WHERE local_path = ?`, localPath)
	repo, err := scanRepository(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Repository{}, false, nil
		}
		return domain.Repository{}, false, fmt.Errorf("sqlite: find repository by path %s: %w", localPath, err)
	}
	return repo, true, nil
}

// ListRepositories implements kernel.Storage.
func (s *Store) ListRepositories(ctx context.Context) ([]domain.Repository, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+repositoryColumns+` FROM repositories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list repositories: %w", err)
	}
	defer rows.Close()

	out := []domain.Repository{}
	for rows.Next() {
		repo, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan repository: %w", err)
		}
		out = append(out, repo)
	}
	return out, rows.Err()
}

// DeleteRepository implements kernel.Storage. Removing a repository's
// documents is the caller's job (see cli.WorkspaceDetach) — this only
// drops the registration row, so a partially-deleted repository is
// visibly still registered rather than silently orphaning its documents.
func (s *Store) DeleteRepository(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM index_state WHERE repository_id = ?`, id); err != nil {
		return fmt.Errorf("sqlite: delete index state %s: %w", id, err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id); err != nil {
		return fmt.Errorf("sqlite: delete repository %s: %w", id, err)
	}
	return nil
}
