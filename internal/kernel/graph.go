package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// Reference is an unresolved cross-document pointer found in a
// CanonicalDocument's Metadata (e.g. a `supersedes: 0001` front-matter
// field). See RFC-0003 and GRAPH.md.
type Reference struct {
	RepositoryID string
	// DocType scopes resolution to documents of the same Knowledge Type
	// as the referencing document (an RFC's `supersedes` should resolve
	// against other RFCs, not any document sharing a number) — the
	// "namespace" in "namespace-aware, not filesystem-directory scoped."
	DocType domain.DocType
	Value   string // raw front-matter value, e.g. "0001" or a repo-relative path
}

// ReferenceResolver resolves a Reference to a target document's stable
// id, within a namespace (repository + doc type) — not by filesystem
// directory location, so moving a file doesn't break references to it.
// Pluggable so a future Source/Knowledge Type can supply its own
// resolution convention; internal/graph.DefaultResolver is the only
// implementation in this pass.
type ReferenceResolver interface {
	Resolve(ctx context.Context, ref Reference) (documentID string, ok bool, err error)
}
