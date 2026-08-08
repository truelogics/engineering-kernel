package kernel

import (
	"context"
	"time"

	"github.com/truelogics/ai-memory/internal/domain"
)

// IndexStatus is IndexState's coarse health signal for `eng status`/`eng
// doctor`.
type IndexStatus string

const (
	IndexStatusClean    IndexStatus = "clean"
	IndexStatusStale    IndexStatus = "stale"
	IndexStatusIndexing IndexStatus = "indexing"
	IndexStatusError    IndexStatus = "error"
)

// IndexState is per-Repository index health — see DATABASE.md's
// `index_state` table.
type IndexState struct {
	RepositoryID           string
	DocumentCount          int
	LastFullIndexAt        time.Time
	LastIncrementalIndexAt time.Time
	Status                 IndexStatus
}

// SearchOptions filters a Storage-level search. See DATABASE.md's
// `document_chunks`/FTS index.
type SearchOptions struct {
	RepositoryID string
	DocType      domain.DocType
	Limit        int
}

// ChunkMatch is one raw hit from Storage's full-text index, before Search
// enriches it with related-document ranking.
type ChunkMatch struct {
	Chunk    domain.Chunk
	Document domain.CanonicalDocument
	Score    float64
	Snippet  string
}

// Storage persists documents, chunks, metadata, tags, and relationships,
// and answers full-text queries over them. The only component that
// touches SQLite — every other component only ever calls this interface.
// See ARCHITECTURE.md and DATABASE.md.
type Storage interface {
	// Repositories
	PutRepository(ctx context.Context, repo domain.Repository) error
	GetRepository(ctx context.Context, id string) (domain.Repository, error)
	FindRepositoryByPath(ctx context.Context, localPath string) (domain.Repository, bool, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
	// DeleteRepository removes a repository's registration. The caller
	// deletes its documents first — see cli.WorkspaceDetach.
	DeleteRepository(ctx context.Context, id string) error

	// Documents
	PutDocument(ctx context.Context, doc domain.CanonicalDocument) error
	GetDocument(ctx context.Context, id string) (domain.CanonicalDocument, error)
	FindDocumentByPath(ctx context.Context, repositoryID, path string) (domain.CanonicalDocument, bool, error)
	ListDocuments(ctx context.Context, repositoryID string) ([]domain.CanonicalDocument, error)
	// DeleteDocument removes a document and everything referencing it
	// (chunks, FTS index, tags, relationships) as explicit application-layer
	// deletes inside one transaction — not ON DELETE CASCADE (see
	// DATABASE.md's open question, resolved this way in RFC-0003). The
	// first hard-delete this kernel performs; `eng sync`'s reason to exist.
	DeleteDocument(ctx context.Context, documentID string) error

	// Chunks (replaces all chunks for a document — Chunker's strategy,
	// not Storage's, decides how many and how they're split)
	PutChunks(ctx context.Context, documentID string, chunks []domain.Chunk) error
	ListChunks(ctx context.Context, documentID string) ([]domain.Chunk, error)

	// Tags
	PutTags(ctx context.Context, documentID string, tags []domain.Tag) error
	ListTags(ctx context.Context, documentID string) ([]domain.Tag, error)
	// FindDocumentsByTag returns other documents in repositoryID sharing
	// the given tag (key, value), excluding excludeDocumentID. Backs
	// Search's "related by shared tags" fallback (ARCHITECTURE.md) for
	// when explicit Relationships haven't been authored.
	FindDocumentsByTag(ctx context.Context, repositoryID, key, value, excludeDocumentID string) ([]domain.CanonicalDocument, error)

	// Relationships
	PutRelationship(ctx context.Context, rel domain.Relationship) error
	ListRelationships(ctx context.Context, documentID string) ([]domain.Relationship, error)
	// TraverseRelationships returns document ids reachable from
	// documentID within depth hops — a bounded `WITH RECURSIVE` query,
	// not a second graph-storage engine (RFC-0003/GRAPH.md). Excludes
	// documentID itself. internal/graph.Graph wraps this with edge
	// hydration.
	TraverseRelationships(ctx context.Context, documentID string, depth int) ([]string, error)

	// Index state
	GetIndexState(ctx context.Context, repositoryID string) (IndexState, bool, error)
	PutIndexState(ctx context.Context, state IndexState) error

	// SearchChunks is the raw full-text query Search's implementation
	// builds ranking and related-file logic on top of. Storage must not
	// decide relevance ranking beyond what the underlying index engine
	// gives it for free — that judgment belongs to Search.
	SearchChunks(ctx context.Context, query string, opts SearchOptions) ([]ChunkMatch, error)

	// Close releases the underlying connection/file handle.
	Close() error
}
