// Package graph implements relationship extraction and traversal —
// Step 8's Milestones 1-2 (RFC-0003, GRAPH.md). No new storage engine:
// extraction turns front-matter references into domain.Relationship rows
// via kernel.Storage; traversal is a bounded walk over the same rows.
package graph

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// DefaultResolver is the only kernel.ReferenceResolver implementation in
// this pass — a convention-based resolver, namespace-aware (repository +
// doc type), not filesystem-directory scoped.
type DefaultResolver struct {
	Storage kernel.Storage
}

var _ kernel.ReferenceResolver = (*DefaultResolver)(nil)

// NewResolver returns a DefaultResolver backed by storage.
func NewResolver(storage kernel.Storage) *DefaultResolver {
	return &DefaultResolver{Storage: storage}
}

// Resolve implements kernel.ReferenceResolver.
func (r *DefaultResolver) Resolve(ctx context.Context, ref kernel.Reference) (string, bool, error) {
	value := strings.TrimSpace(ref.Value)
	if value == "" {
		return "", false, nil
	}

	// A reference that already looks like a path resolves deterministically
	// — no lookup needed, and no coupling to the referencing document's own
	// location (repo-root-relative, not relative to the referencing file).
	if looksLikePath(value) {
		return domain.DocumentID(ref.RepositoryID, cleanReferencePath(value)), true, nil
	}

	// Numeric-style reference (this repo's own convention: `supersedes:
	// 0001`). Namespace-aware: search documents in the same repository
	// with the same Knowledge Type as the referencing document — an RFC's
	// `supersedes` resolves against other RFCs, not any document that
	// happens to share a number — regardless of which directory either
	// document lives in.
	docs, err := r.Storage.ListDocuments(ctx, ref.RepositoryID)
	if err != nil {
		return "", false, fmt.Errorf("graph: resolve %q: %w", value, err)
	}
	for _, d := range docs {
		if d.Type != ref.DocType {
			continue
		}
		if numericPrefixMatches(d.Path, value) {
			return d.ID, true, nil
		}
	}
	return "", false, nil
}

// looksLikePath reports whether v is a repo-relative path reference
// rather than a bare numeric/convention id — a URL doesn't count (not a
// repo-relative reference).
func looksLikePath(v string) bool {
	if v == "" || strings.Contains(v, "://") {
		return false
	}
	return strings.Contains(v, "/") || strings.HasSuffix(strings.ToLower(v), ".md")
}

// cleanReferencePath trims a leading "./" so "./docs/x.md" and
// "docs/x.md" resolve to the same document id.
func cleanReferencePath(v string) string {
	return path.Clean(strings.TrimPrefix(strings.TrimSpace(v), "./"))
}

// numericPrefixMatches reports whether p's filename starts with number
// followed by a separator (or is an exact match) — this repo's own
// `NNNN-short-title.md` convention (RFCs, ADRs).
func numericPrefixMatches(p, number string) bool {
	base := path.Base(p)
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem == number {
		return true
	}
	return strings.HasPrefix(base, number+"-") || strings.HasPrefix(base, number+"_")
}
