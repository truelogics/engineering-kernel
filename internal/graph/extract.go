package graph

import (
	"context"
	"sort"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// referencesOnlyFromThisSide are the front-matter fields Extract resolves
// into a Relationship attributed to (from = the referencing document).
// Fields like `superseded_by` describe an edge from the *other*
// document's perspective — resolving those here would mean writing a
// relationship row whose from_document_id isn't the document currently
// being saved, which PutDocument's per-document replace semantics don't
// safely support (the owning document's next re-index wouldn't know to
// preserve or reclaim it). Deliberately excluded in this pass rather than
// half-handled; see RFC-0003.
var excludedFields = map[string]bool{
	"superseded_by": true,
	"resulting_adr": true, // same issue: describes an edge from the ADR's side
}

// Extract resolves doc's reference-shaped Metadata fields into
// Relationships, using resolver. Runs after Normalize, before Chunk
// (RFC-0003) — callers append the result to doc.Relationships before
// Storage.PutDocument. Every returned Relationship has doc.ID as its
// From side.
func Extract(ctx context.Context, doc domain.CanonicalDocument, resolver kernel.ReferenceResolver) ([]domain.Relationship, error) {
	seen := map[string]bool{}
	var out []domain.Relationship

	add := func(value string, relType domain.RelationshipType) error {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		targetID, ok, err := resolver.Resolve(ctx, kernel.Reference{
			RepositoryID: doc.RepositoryID,
			DocType:      doc.Type,
			Value:        value,
		})
		if err != nil {
			return err
		}
		if !ok || targetID == doc.ID {
			return nil
		}
		rel, err := domain.NewRelationship(doc.ID, targetID, relType, domain.RelationshipExplicit)
		if err != nil {
			return err
		}
		if seen[rel.ID] {
			return nil
		}
		seen[rel.ID] = true
		out = append(out, rel)
		return nil
	}

	if v, ok := doc.Metadata.Get("supersedes"); ok {
		if err := add(v, domain.RelationshipSupersedes); err != nil {
			return nil, err
		}
	}

	// Generic path-like references in any other field — sorted keys so
	// extraction order (and therefore which duplicate "wins" the seen-map,
	// not that content should differ) is deterministic for tests.
	keys := make([]string, 0, len(doc.Metadata))
	for k := range doc.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "supersedes" || excludedFields[k] {
			continue
		}
		v, _ := doc.Metadata.Get(k)
		if !looksLikePath(v) {
			continue
		}
		if err := add(v, domain.RelationshipReferences); err != nil {
			return nil, err
		}
	}

	return out, nil
}
