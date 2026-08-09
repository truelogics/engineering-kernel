package filesystem

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/truelogics/ai-memory/internal/domain"
)

// repoWithIgnores builds a real repository, because the behaviour under test is
// git's interpretation of .gitignore and a fake would only test my
// reading of it.
func repoWithIgnores(t *testing.T, ignore string, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")

	if ignore != "" {
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignore), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func collect(t *testing.T, dir string) []string {
	t.Helper()
	docs, err := New().Collect(context.Background(), domain.Repository{ID: "r", LocalPath: dir})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	return paths
}

func contains(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// TestCollectSkipsGitIgnoredFiles is Sprint 12's finding as a test. A
// real monorepo offered 22,445 markdown files to a collector whose
// fixed skip list caught node_modules and vendor and nothing else;
// 21,690 were tool caches under .claude/, and the project tracked 726.
func TestCollectSkipsGitIgnoredFiles(t *testing.T) {
	dir := repoWithIgnores(t, ".claude/\nbuild/\n", map[string]string{
		"README.md":                "real",
		"docs/adr/0001.md":         "real",
		".claude/skills/cache.md":  "tool cache",
		".claude/plugins/other.md": "tool cache",
		"build/generated.md":       "build output",
	})

	got := collect(t, dir)
	for _, want := range []string{"README.md", "docs/adr/0001.md"} {
		if !contains(got, want) {
			t.Errorf("dropped a tracked document %q: %v", want, got)
		}
	}
	for _, unwanted := range []string{".claude/skills/cache.md", ".claude/plugins/other.md", "build/generated.md"} {
		if contains(got, unwanted) {
			t.Errorf("collected an ignored file %q — the project has declared it is not part of it", unwanted)
		}
	}
	if len(got) != 3 { // README, the ADR, and .gitignore's sibling count — see below
		t.Logf("collected: %v", got)
	}
}

// TestCollectKeepsEverythingWhenGitCannotAnswer pins the fallback
// direction. Indexing less than asked, silently, is the worse failure:
// the ignore filter narrows scope and is not a correctness guarantee.
func TestCollectKeepsEverythingWhenGitCannotAnswer(t *testing.T) {
	dir := t.TempDir() // not a git repository
	for _, name := range []string{"README.md", "notes/a.md"} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := collect(t, dir)
	if len(got) != 2 {
		t.Errorf("got %v, want both files — outside a repository nothing is ignored", got)
	}
}

// TestCollectStillHonoursSkipDirs: node_modules is skipped during the
// walk, before git is consulted, so a repository that does not ignore it
// still does not get 25,000 dependency READMEs.
func TestCollectStillHonoursSkipDirs(t *testing.T) {
	dir := repoWithIgnores(t, "", map[string]string{
		"README.md":                     "real",
		"node_modules/left-pad/API.md":  "dependency",
		"vendor/github.com/x/README.md": "dependency",
	})

	got := collect(t, dir)
	if !contains(got, "README.md") {
		t.Errorf("dropped the real document: %v", got)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want only README.md", got)
	}
}

func TestDropIgnoredHandlesNoPaths(t *testing.T) {
	if got := dropIgnored(context.Background(), t.TempDir(), nil); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
