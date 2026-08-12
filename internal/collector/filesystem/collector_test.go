package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
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

func TestCollectFindsMarkdownOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello")
	writeFile(t, dir, "notes.markdown", "# Notes")
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "docs/ARCHITECTURE.md", "# Architecture")
	writeFile(t, dir, ".git/HEAD", "ref: refs/heads/main")
	writeFile(t, dir, "node_modules/pkg/README.md", "# should be skipped")

	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	c := New()
	docs, err := c.Collect(context.Background(), repo)
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	want := []string{"README.md", "docs/ARCHITECTURE.md", "notes.markdown"}
	if len(paths) != len(want) {
		t.Fatalf("Collect: got %d docs %v, want %d %v", len(paths), paths, len(want), want)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Fatalf("Collect: paths[%d] = %q, want %q (full: %v)", i, paths[i], p, paths)
		}
	}
}

func TestCollectSetsSourceIDAndBytes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello World")

	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	docs, err := New().Collect(context.Background(), repo)
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("Collect: got %d docs, want 1", len(docs))
	}
	if docs[0].SourceID != repo.ID {
		t.Fatalf("Collect: SourceID = %q, want repo.ID %q", docs[0].SourceID, repo.ID)
	}
	if string(docs[0].Bytes) != "# Hello World" {
		t.Fatalf("Collect: Bytes = %q, want %q", docs[0].Bytes, "# Hello World")
	}
	if docs[0].FetchedAt.IsZero() {
		t.Fatal("Collect: expected FetchedAt to be set")
	}
}

func TestCollectRejectsEmptyLocalPath(t *testing.T) {
	repo := domain.Repository{ID: "repo-1", LocalPath: ""}
	if _, err := New().Collect(context.Background(), repo); err == nil {
		t.Fatal("Collect: expected error for empty LocalPath")
	}
}

func TestCollectWithZeroValueCollectorUsesDefaultSkipDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello")
	writeFile(t, dir, "vendor/pkg/README.md", "# should be skipped")

	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	var c Collector // zero value, SkipDirs is nil
	docs, err := c.Collect(context.Background(), repo)
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}
	if len(docs) != 1 || docs[0].Path != "README.md" {
		t.Fatalf("Collect with zero-value Collector: got %+v, want only README.md", docs)
	}
}

func TestCollectStopsOnCancelledContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello")

	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := New().Collect(ctx, repo); err == nil {
		t.Fatal("Collect: expected error for already-cancelled context")
	}
}

func TestCollectEmptyRepoReturnsNoDocs(t *testing.T) {
	dir := t.TempDir()
	repo, err := domain.NewRepository("ws-1", "empty-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	docs, err := New().Collect(context.Background(), repo)
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("Collect: got %d docs, want 0", len(docs))
	}
}
