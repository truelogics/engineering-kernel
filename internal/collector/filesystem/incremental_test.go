package filesystem

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// gitRepo sets up a real git repository in a temp dir with an initial
// commit, so incremental collection tests exercise real `git diff`/`git
// status` output, not a mocked approximation of it.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeFile(t, dir, "README.md", "# Initial\n")
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

func headCommit(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(bytesTrimSpace(out))
}

func bytesTrimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func TestCurrentRefReturnsHEAD(t *testing.T) {
	dir := gitRepo(t)
	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	ref, err := New().CurrentRef(context.Background(), repo)
	if err != nil {
		t.Fatalf("CurrentRef: unexpected error: %v", err)
	}
	want := headCommit(t, dir)
	if ref != want {
		t.Fatalf("CurrentRef = %q, want %q", ref, want)
	}
}

func TestCurrentRefNonGitReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // not a git repo
	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	ref, err := New().CurrentRef(context.Background(), repo)
	if err != nil {
		t.Fatalf("CurrentRef: unexpected error for non-git dir: %v", err)
	}
	if ref != "" {
		t.Fatalf("CurrentRef = %q, want empty for a non-git directory", ref)
	}
}

func TestCollectChangedEmptySinceIsFullCollect(t *testing.T) {
	dir := gitRepo(t)
	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	changed, deleted, err := New().CollectChanged(context.Background(), repo, "")
	if err != nil {
		t.Fatalf("CollectChanged: unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("CollectChanged with empty since: deleted = %v, want none", deleted)
	}
	if len(changed) != 1 || changed[0].Path != "README.md" {
		t.Fatalf("CollectChanged with empty since = %+v, want just README.md (full collect)", changed)
	}
}

func TestCollectChangedDetectsCommittedAndUncommittedChanges(t *testing.T) {
	dir := gitRepo(t)
	since := headCommit(t, dir)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Committed change: modify README.md and add a new committed file.
	writeFile(t, dir, "README.md", "# Changed\n")
	writeFile(t, dir, "docs/NEW.md", "# New\n")
	run("add", ".")
	run("commit", "-q", "-m", "second commit")

	// Uncommitted change: a brand new untracked file.
	writeFile(t, dir, "UNTRACKED.md", "# Untracked\n")

	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	changed, deleted, err := New().CollectChanged(context.Background(), repo, since)
	if err != nil {
		t.Fatalf("CollectChanged: unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("CollectChanged: deleted = %v, want none", deleted)
	}

	var paths []string
	for _, c := range changed {
		paths = append(paths, c.Path)
	}
	sort.Strings(paths)
	want := []string{"README.md", "UNTRACKED.md", "docs/NEW.md"}
	if len(paths) != len(want) {
		t.Fatalf("CollectChanged paths = %v, want %v", paths, want)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Fatalf("CollectChanged paths = %v, want %v", paths, want)
		}
	}
}

func TestCollectChangedDetectsDeletion(t *testing.T) {
	dir := gitRepo(t)
	since := headCommit(t, dir)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "delete README")

	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	changed, deleted, err := New().CollectChanged(context.Background(), repo, since)
	if err != nil {
		t.Fatalf("CollectChanged: unexpected error: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("CollectChanged: changed = %+v, want none", changed)
	}
	if len(deleted) != 1 || deleted[0] != "README.md" {
		t.Fatalf("CollectChanged: deleted = %v, want [README.md]", deleted)
	}
}

func TestCollectChangedNoChangesReturnsEmpty(t *testing.T) {
	dir := gitRepo(t)
	since := headCommit(t, dir)
	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	changed, deleted, err := New().CollectChanged(context.Background(), repo, since)
	if err != nil {
		t.Fatalf("CollectChanged: unexpected error: %v", err)
	}
	if len(changed) != 0 || len(deleted) != 0 {
		t.Fatalf("CollectChanged with nothing changed = (%+v, %v), want both empty", changed, deleted)
	}
}

func TestCollectChangedFallsBackToFullCollectOnUnknownCommit(t *testing.T) {
	dir := gitRepo(t)
	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	changed, deleted, err := New().CollectChanged(context.Background(), repo, "0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("CollectChanged: unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("CollectChanged fallback: deleted = %v, want none", deleted)
	}
	if len(changed) != 1 || changed[0].Path != "README.md" {
		t.Fatalf("CollectChanged fallback = %+v, want full collect (README.md)", changed)
	}
}

func TestCollectChangedNotAGitRepoFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello\n")
	repo, err := domain.NewRepository("ws-1", "test-repo", dir)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	changed, deleted, err := New().CollectChanged(context.Background(), repo, "somecommit")
	if err != nil {
		t.Fatalf("CollectChanged: unexpected error: %v", err)
	}
	if len(deleted) != 0 || len(changed) != 1 {
		t.Fatalf("CollectChanged on non-git dir = (%+v, %v), want full collect fallback", changed, deleted)
	}
}
