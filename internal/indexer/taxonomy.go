package indexer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// loadTaxonomy reads a repository's own statement about what its
// directories hold (RFC-0007). Absent is the normal case and not an
// error; malformed is an error, because a repository trying to say
// something and being ignored is worse than one that said nothing.
func loadTaxonomy(repo domain.Repository) (domain.Taxonomy, error) {
	if repo.LocalPath == "" {
		return domain.Taxonomy{}, nil
	}
	content, err := os.ReadFile(filepath.Join(repo.LocalPath, domain.TaxonomyFile))
	if os.IsNotExist(err) {
		return domain.Taxonomy{}, nil
	}
	if err != nil {
		return domain.Taxonomy{}, fmt.Errorf("indexer: read %s: %w", domain.TaxonomyFile, err)
	}
	t, err := domain.ParseTaxonomy(content)
	if err != nil {
		return domain.Taxonomy{}, fmt.Errorf("indexer: %s: %w", domain.TaxonomyFile, err)
	}
	return t, nil
}

// applyTaxonomy classifies a document the kernel could not.
//
// Only `unknown` is eligible. Front matter is what a document says about
// itself and a taxonomy is what the repository says about a directory —
// the specific claim wins over the general one. This also means no
// document that classifies today can be reclassified by adding a
// taxonomy, which is what makes the change safe to land under every
// existing consumer (RFC-0007).
func applyTaxonomy(t domain.Taxonomy, doc *domain.CanonicalDocument) {
	if t.Empty() || doc.Type != domain.DocTypeUnknown {
		return
	}
	if docType, ok := t.TypeFor(doc.Path); ok {
		doc.Type = docType
	}
}
