package kernel

import (
	"context"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// Normalizer reconciles whatever a Parser produced into one guaranteed
// CanonicalDocument shape, regardless of source-format quirks. v1's
// implementation is a pass-through (markdown's Parser output is already
// canonical) — the interface exists so the seam is there once a second,
// non-markdown Parser needs it. See RFC-0002's "why Normalization."
type Normalizer interface {
	// Normalize must not fetch content or know about specific
	// Source/Parser types — it operates generically on whatever
	// structured document it's handed.
	Normalize(ctx context.Context, doc domain.CanonicalDocument) (domain.CanonicalDocument, error)
}
