// Package normalizer implements kernel.Normalizer. v1 is close to a
// pass-through — markdown's Parser output is already canonical — but it
// still enforces real invariants (defaults, deduplication, path
// cleaning) so a future non-markdown Parser's output is held to the same
// guarantee without Chunker/Indexer/Storage/Search ever special-casing
// it. See RFC-0002's "why Normalization."
package normalizer

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// Normalizer reconciles a Parser's output into one guaranteed
// CanonicalDocument shape.
type Normalizer struct {
	// Now is injectable for deterministic tests; defaults to time.Now.
	Now func() time.Time
}

var _ kernel.Normalizer = (*Normalizer)(nil)

// New returns a Normalizer using the real wall clock.
func New() *Normalizer {
	return &Normalizer{Now: time.Now}
}

// Normalize implements kernel.Normalizer.
func (n *Normalizer) Normalize(ctx context.Context, doc domain.CanonicalDocument) (domain.CanonicalDocument, error) {
	if strings.TrimSpace(doc.ID) == "" {
		return domain.CanonicalDocument{}, errors.New("normalizer: document has no ID")
	}
	if strings.TrimSpace(doc.RepositoryID) == "" {
		return domain.CanonicalDocument{}, errors.New("normalizer: document has no RepositoryID")
	}

	doc.Path = normalizePath(doc.Path)
	if doc.Path == "" {
		return domain.CanonicalDocument{}, errors.New("normalizer: document has no path")
	}

	if doc.Metadata == nil {
		doc.Metadata = domain.NewMetadata()
	}
	doc.Tags = dedupeTags(doc.Tags)
	doc.Relationships = dedupeRelationships(doc.Relationships)

	if doc.Type == "" {
		doc.Type = domain.DocTypeUnknown
	}
	if strings.TrimSpace(doc.Title) == "" {
		doc.Title = doc.Path
	}

	now := n.now()
	if doc.GitUpdatedAt.IsZero() {
		doc.GitUpdatedAt = now
	}
	doc.IndexedAt = now

	return doc, nil
}

func (n *Normalizer) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// normalizePath cleans a logical (already forward-slash) document path:
// trims whitespace, drops a leading "./", and resolves "." / ".." segments.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// dedupeTags removes exact key+value duplicates, preserving first-seen
// order.
func dedupeTags(tags []domain.Tag) []domain.Tag {
	seen := make(map[string]bool, len(tags))
	out := make([]domain.Tag, 0, len(tags))
	for _, t := range tags {
		key := t.Key + "\x00" + t.Value
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

// dedupeRelationships removes duplicate edges by ID (Relationship.ID is
// content-derived, so equal ID means equal edge).
func dedupeRelationships(rels []domain.Relationship) []domain.Relationship {
	seen := make(map[string]bool, len(rels))
	out := make([]domain.Relationship, 0, len(rels))
	for _, r := range rels {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out
}
