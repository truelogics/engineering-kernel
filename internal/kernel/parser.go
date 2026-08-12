package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// Parser converts a RawDocument into a structured document — ideally
// already domain.CanonicalDocument-shaped, though not guaranteed until
// Normalizer runs. One Parser implementation per format (v1: markdown) —
// see RFC-0002's "why Parsers per format."
type Parser interface {
	// CanParse reports whether p knows how to handle raw (e.g. by
	// extension). Indexer uses this to pick a Parser without every
	// Parser needing to know about every other one.
	CanParse(raw domain.RawDocument) bool

	// Parse turns raw into a structured document. Must not chunk,
	// persist, or assume a Normalizer will fix mistakes downstream.
	Parse(ctx context.Context, raw domain.RawDocument) (domain.CanonicalDocument, error)
}
