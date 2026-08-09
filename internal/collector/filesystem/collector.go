// Package filesystem implements kernel.Collector against a local git
// repository — v1's only Collector. See ARCHITECTURE.md and
// INTERFACES.md.
package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/truelogics/ai-memory/internal/domain"
	"github.com/truelogics/ai-memory/internal/kernel"
)

// Collector walks a Repository's LocalPath and reads every file worth
// indexing. v1 collects markdown only (.md, .markdown) — READMEs and ADRs
// are markdown files distinguished later by Parser/Normalizer via
// Knowledge Type, not by a separate collection rule here.
type Collector struct {
	// SkipDirs are directory names never descended into. Defaults to
	// a sane set (.git, node_modules, vendor) when nil.
	SkipDirs map[string]bool
}

var _ kernel.Collector = (*Collector)(nil)

// New returns a Collector with default SkipDirs.
func New() *Collector {
	return &Collector{
		SkipDirs: map[string]bool{
			".git":         true,
			"node_modules": true,
			"vendor":       true,
		},
	}
}

// Collect walks repo.LocalPath and returns a RawDocument for every
// markdown file found. Files are returned in deterministic (sorted path)
// order so Indexer's output is reproducible.
func (c *Collector) Collect(ctx context.Context, repo domain.Repository) ([]domain.RawDocument, error) {
	if strings.TrimSpace(repo.LocalPath) == "" {
		return nil, fmt.Errorf("filesystem: repository %q has no local path", repo.ID)
	}
	root := repo.LocalPath

	var paths []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != root && c.skipDirs()[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !isMarkdown(d.Name()) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("filesystem: walk %s: %w", root, walkErr)
	}
	// SkipDirs alone cannot express what a project considers its own —
	// see gitignore.go. Advisory: if git cannot answer, nothing is
	// dropped.
	paths = dropIgnored(ctx, root, paths)
	sort.Strings(paths)

	docs := make([]domain.RawDocument, 0, len(paths))
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("filesystem: read %s: %w", p, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		raw, err := domain.NewRawDocument(repo.ID, filepath.ToSlash(rel), content)
		if err != nil {
			return nil, fmt.Errorf("filesystem: %s: %w", rel, err)
		}
		docs = append(docs, raw)
	}
	return docs, nil
}

func (c *Collector) skipDirs() map[string]bool {
	if c.SkipDirs != nil {
		return c.SkipDirs
	}
	return New().SkipDirs
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}
