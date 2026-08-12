package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/truelogics/engineering-kernel/pkg/memory"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestSDKEndToEnd exercises exactly the surface RFC-0004 promises a
// consumer like ai-review: Open a workspace, register a repository, Index
// it, Search it, and ask for Context — without touching anything under
// internal/.
func TestSDKEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "---\ndoc: README\nstatus: living\n---\n\n# Demo Project\n\nWe chose JWT for stateless authentication across services.\n")
	writeFile(t, dir, "docs/ARCHITECTURE.md", "# Architecture\n\nThis document describes the pipeline architecture of the system.\n")
	writeFile(t, dir, "engineering/ADR/0003-jwt.md", "# ADR 0003\n\nAuthentication decision: use JWT.\n")

	m, err := memory.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer m.Close()

	repo, err := m.AddRepository(ctx, dir)
	if err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if repo.ID == "" {
		t.Fatal("AddRepository: expected a non-empty Repository.ID")
	}
	if repo.LocalPath != dir {
		t.Errorf("AddRepository: LocalPath = %q, want %q", repo.LocalPath, dir)
	}

	result, err := m.Index(ctx, repo)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if result.Scanned != 3 || result.Added != 3 {
		t.Errorf("Index result = %+v, want 3 scanned, 3 added", result)
	}

	hits, err := m.Search(ctx, "architecture", memory.SearchOptions{})
	if err != nil {
		t.Fatalf("Search(architecture): %v", err)
	}
	if len(hits) == 0 || hits[0].Path == "" {
		t.Fatalf("Search(architecture) = %+v, want at least one hit with a Path", hits)
	}
	found := false
	for _, h := range hits {
		if filepath.Base(h.Path) == "ARCHITECTURE.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("Search(architecture) = %+v, want a hit on ARCHITECTURE.md", hits)
	}

	pkg, err := m.Context(ctx, "Review authentication PR")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if pkg.Task != "Review authentication PR" {
		t.Errorf("Context.Task = %q, want the original task string", pkg.Task)
	}
	if len(pkg.ADRs) == 0 {
		t.Errorf("Context.ADRs = %+v, want a hit on the ADR", pkg.ADRs)
	}
	if len(pkg.RelevantFiles) == 0 {
		t.Errorf("Context.RelevantFiles = %+v, want at least the architecture doc", pkg.RelevantFiles)
	}
	// RelatedIssues/RelatedPRs are always present, always empty (RFC-0004) —
	// no Issue/PR ingestion exists — distinct from being nil/omitted.
	if pkg.RelatedIssues == nil {
		t.Error("Context.RelatedIssues = nil, want a non-nil empty slice")
	}
	if pkg.RelatedPRs == nil {
		t.Error("Context.RelatedPRs = nil, want a non-nil empty slice")
	}
}

// TestAddRepositoryIsIdempotent mirrors internal/cli's `eng init` guarantee
// through the public API: registering the same path twice returns the
// same Repository rather than erroring or duplicating it.
func TestAddRepositoryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	m, err := memory.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer m.Close()

	first, err := m.AddRepository(ctx, dir)
	if err != nil {
		t.Fatalf("AddRepository (first): %v", err)
	}
	second, err := m.AddRepository(ctx, dir)
	if err != nil {
		t.Fatalf("AddRepository (second): %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("AddRepository ids = %q, %q, want the same repository both times", first.ID, second.ID)
	}
}

func TestSearchNoMatches(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello\n\nNothing relevant here.\n")

	m, err := memory.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer m.Close()

	repo, err := m.AddRepository(ctx, dir)
	if err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if _, err := m.Index(ctx, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	hits, err := m.Search(ctx, "zzzznonexistent", memory.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search(zzzznonexistent) = %+v, want no hits", hits)
	}
}
