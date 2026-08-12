package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

const documentColumns = `id, repository_id, path, doc_type, title, front_matter, body, content_hash, git_author, git_updated_at, indexed_at`

func scanDocument(sc scanner) (domain.CanonicalDocument, error) {
	var d domain.CanonicalDocument
	var docType, frontMatter string
	var gitUpdatedAt, indexedAt sql.NullTime
	if err := sc.Scan(&d.ID, &d.RepositoryID, &d.Path, &docType, &d.Title, &frontMatter, &d.Content, &d.ContentHash, &d.GitAuthor, &gitUpdatedAt, &indexedAt); err != nil {
		return domain.CanonicalDocument{}, err
	}
	d.Type = domain.DocType(docType)
	d.GitUpdatedAt = scanTime(gitUpdatedAt)
	d.IndexedAt = scanTime(indexedAt)

	meta := domain.NewMetadata()
	if frontMatter != "" {
		if err := json.Unmarshal([]byte(frontMatter), &meta); err != nil {
			return domain.CanonicalDocument{}, fmt.Errorf("decode front_matter for %s: %w", d.ID, err)
		}
	}
	d.Metadata = meta
	return d, nil
}

// PutDocument implements kernel.Storage. Writes the document row plus its
// Tags and Relationships in one transaction — see RFC-0002's "why Indexer
// stays separate from Storage": this method is Storage's mechanics
// (atomic write), not a policy decision about what's stale.
func (s *Store) PutDocument(ctx context.Context, doc domain.CanonicalDocument) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: put document %s: begin tx: %w", doc.ID, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	frontMatter, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("sqlite: put document %s: marshal metadata: %w", doc.ID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO documents (`+documentColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			repository_id=excluded.repository_id,
			path=excluded.path,
			doc_type=excluded.doc_type,
			title=excluded.title,
			front_matter=excluded.front_matter,
			body=excluded.body,
			content_hash=excluded.content_hash,
			git_author=excluded.git_author,
			git_updated_at=excluded.git_updated_at,
			indexed_at=excluded.indexed_at
	`, doc.ID, doc.RepositoryID, doc.Path, string(doc.Type), doc.Title, string(frontMatter), doc.Content, doc.ContentHash, doc.GitAuthor, nullTime(doc.GitUpdatedAt), nullTime(doc.IndexedAt)); err != nil {
		return fmt.Errorf("sqlite: put document %s: %w", doc.ID, err)
	}

	if err := replaceTags(ctx, tx, doc.ID, doc.Tags); err != nil {
		return fmt.Errorf("sqlite: put document %s: %w", doc.ID, err)
	}
	if err := replaceRelationships(ctx, tx, doc.ID, doc.Relationships); err != nil {
		return fmt.Errorf("sqlite: put document %s: %w", doc.ID, err)
	}

	return tx.Commit()
}

// GetDocument implements kernel.Storage.
func (s *Store) GetDocument(ctx context.Context, id string) (domain.CanonicalDocument, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+documentColumns+` FROM documents WHERE id = ?`, id)
	doc, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CanonicalDocument{}, fmt.Errorf("sqlite: document %s not found", id)
		}
		return domain.CanonicalDocument{}, fmt.Errorf("sqlite: get document %s: %w", id, err)
	}
	if err := s.attach(ctx, &doc); err != nil {
		return domain.CanonicalDocument{}, err
	}
	return doc, nil
}

// FindDocumentByPath implements kernel.Storage.
func (s *Store) FindDocumentByPath(ctx context.Context, repositoryID, path string) (domain.CanonicalDocument, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+documentColumns+` FROM documents WHERE repository_id = ? AND path = ?`, repositoryID, path)
	doc, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CanonicalDocument{}, false, nil
		}
		return domain.CanonicalDocument{}, false, fmt.Errorf("sqlite: find document %s/%s: %w", repositoryID, path, err)
	}
	if err := s.attach(ctx, &doc); err != nil {
		return domain.CanonicalDocument{}, false, err
	}
	return doc, true, nil
}

// ListDocuments implements kernel.Storage.
func (s *Store) ListDocuments(ctx context.Context, repositoryID string) ([]domain.CanonicalDocument, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+documentColumns+` FROM documents WHERE repository_id = ? ORDER BY path`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list documents for %s: %w", repositoryID, err)
	}
	defer rows.Close()

	out := []domain.CanonicalDocument{}
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan document: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.attach(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// attach populates doc.Tags and doc.Relationships from their tables.
func (s *Store) attach(ctx context.Context, doc *domain.CanonicalDocument) error {
	tags, err := s.ListTags(ctx, doc.ID)
	if err != nil {
		return err
	}
	doc.Tags = tags

	rels, err := s.ListRelationships(ctx, doc.ID)
	if err != nil {
		return err
	}
	doc.Relationships = rels
	return nil
}

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func replaceTags(ctx context.Context, tx execer, documentID string, tags []domain.Tag) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tags (id, document_id, key, value) VALUES (?, ?, ?, ?)`,
			tag.ID, documentID, tag.Key, tag.Value); err != nil {
			return fmt.Errorf("insert tag %s: %w", tag.Key, err)
		}
	}
	return nil
}

func replaceRelationships(ctx context.Context, tx execer, documentID string, rels []domain.Relationship) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM relationships WHERE from_document_id = ?`, documentID); err != nil {
		return fmt.Errorf("clear relationships: %w", err)
	}
	for _, rel := range rels {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO relationships (id, from_document_id, to_document_id, relationship_type, source, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, rel.ID, rel.FromDocumentID, rel.ToDocumentID, string(rel.Type), string(rel.Source), time.Now().UTC()); err != nil {
			return fmt.Errorf("insert relationship %s: %w", rel.ID, err)
		}
	}
	return nil
}

// PutTags implements kernel.Storage.
func (s *Store) PutTags(ctx context.Context, documentID string, tags []domain.Tag) error {
	if err := replaceTags(ctx, s.db, documentID, tags); err != nil {
		return fmt.Errorf("sqlite: put tags for %s: %w", documentID, err)
	}
	return nil
}

// ListTags implements kernel.Storage.
func (s *Store) ListTags(ctx context.Context, documentID string) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, document_id, key, value FROM tags WHERE document_id = ? ORDER BY key`, documentID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tags for %s: %w", documentID, err)
	}
	defer rows.Close()

	out := []domain.Tag{}
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.DocumentID, &t.Key, &t.Value); err != nil {
			return nil, fmt.Errorf("sqlite: scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FindDocumentsByTag implements kernel.Storage.
func (s *Store) FindDocumentsByTag(ctx context.Context, repositoryID, key, value, excludeDocumentID string) ([]domain.CanonicalDocument, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT d.id
		FROM tags t
		JOIN documents d ON d.id = t.document_id
		WHERE d.repository_id = ? AND t.key = ? AND t.value = ? AND d.id != ?
	`, repositoryID, key, value, excludeDocumentID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: find documents by tag %s=%s: %w", key, value, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("sqlite: scan document id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]domain.CanonicalDocument, 0, len(ids))
	for _, id := range ids {
		doc, err := s.GetDocument(ctx, id)
		if err != nil {
			continue // best-effort: a dangling tag shouldn't fail the whole lookup
		}
		out = append(out, doc)
	}
	return out, nil
}

// PutRelationship implements kernel.Storage.
func (s *Store) PutRelationship(ctx context.Context, rel domain.Relationship) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO relationships (id, from_document_id, to_document_id, relationship_type, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, rel.ID, rel.FromDocumentID, rel.ToDocumentID, string(rel.Type), string(rel.Source), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("sqlite: put relationship %s: %w", rel.ID, err)
	}
	return nil
}

// ListRelationships implements kernel.Storage — both directions, since a
// document can be either endpoint of an edge.
func (s *Store) ListRelationships(ctx context.Context, documentID string) ([]domain.Relationship, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, from_document_id, to_document_id, relationship_type, source
		FROM relationships
		WHERE from_document_id = ? OR to_document_id = ?
		ORDER BY id
	`, documentID, documentID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list relationships for %s: %w", documentID, err)
	}
	defer rows.Close()

	out := []domain.Relationship{}
	for rows.Next() {
		var r domain.Relationship
		var relType, source string
		if err := rows.Scan(&r.ID, &r.FromDocumentID, &r.ToDocumentID, &relType, &source); err != nil {
			return nil, fmt.Errorf("sqlite: scan relationship: %w", err)
		}
		r.Type = domain.RelationshipType(relType)
		r.Source = domain.RelationshipSource(source)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListDocumentsByType implements kernel.Storage: every document of one
// Knowledge Type across every repository in the workspace. Backs
// scope-selected rule retrieval (RFC-0005), where the question is "which
// rules govern these files" rather than "which documents mention these
// words" — the two have different answers, and only the second is a
// search.
func (s *Store) ListDocumentsByType(ctx context.Context, docType domain.DocType) ([]domain.CanonicalDocument, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+documentColumns+` FROM documents WHERE doc_type = ? ORDER BY path`, string(docType))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list documents by type %s: %w", docType, err)
	}
	defer rows.Close()

	var out []domain.CanonicalDocument
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan document: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.attach(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
