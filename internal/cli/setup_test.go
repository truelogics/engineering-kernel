package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupDetachesAContainerRoot is the decision the install runbook
// asked developers to make by hand, and the one they skipped. A root
// left attached indexes every child repository twice — once under its
// own name and once under the root's — and citations then name the
// wrong repository.
func TestSetupDetachesAContainerRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rules := filepath.Join(root, "engineering")
	writeRepo(t, rules, map[string]string{"rules/logging.md": "---\ndoc: RULE\n---\n\n# Logging\n"})

	var out strings.Builder
	failed, err := prepareWorkspace(ctx, root, []string{rules}, &out)
	if err != nil {
		t.Fatalf("prepareWorkspace: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %v, want none\n%s", failed, out.String())
	}

	names := attachedRepositories(ctx, root)
	if len(names) != 1 || names[0] != "engineering" {
		t.Fatalf("attached = %v, want only [engineering]\n%s", names, out.String())
	}
}

// TestSetupKeepsASingleRepositoryRootAttached is the other half of the
// same rule. A workspace created inside one repository is that
// repository, and detaching it there would produce a workspace holding
// nothing at all.
func TestSetupKeepsASingleRepositoryRootAttached(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeRepo(t, root, map[string]string{"rules/logging.md": "---\ndoc: RULE\n---\n\n# Logging\n"})

	var out strings.Builder
	if _, err := prepareWorkspace(ctx, root, []string{root}, &out); err != nil {
		t.Fatalf("prepareWorkspace: %v", err)
	}

	if names := attachedRepositories(ctx, root); len(names) != 1 {
		t.Fatalf("attached = %v, want the repository itself\n%s", names, out.String())
	}
}

// TestSetupIsRerunnable: running it again must not lose the workspace or
// duplicate what is in it. Re-running is how a developer repoints an
// installation at a rebuilt binary, so it has to be safe.
func TestSetupIsRerunnable(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rules := filepath.Join(root, "engineering")
	writeRepo(t, rules, map[string]string{"rules/logging.md": "---\ndoc: RULE\n---\n\n# Logging\n"})

	var out strings.Builder
	for i := range 2 {
		if _, err := prepareWorkspace(ctx, root, []string{rules}, &out); err != nil {
			t.Fatalf("prepareWorkspace run %d: %v", i+1, err)
		}
	}

	if names := attachedRepositories(ctx, root); len(names) != 1 {
		t.Fatalf("attached after two runs = %v, want one entry\n%s", names, out.String())
	}
}

// TestSetupSurvivesOneBadTarget: a mistyped path must not stop the
// rulebook named alongside it from being indexed. Reporting the failure
// and carrying on is the difference between "fix this one path" and
// "start again".
func TestSetupSurvivesOneBadTarget(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rules := filepath.Join(root, "engineering")
	writeRepo(t, rules, map[string]string{"rules/logging.md": "---\ndoc: RULE\n---\n\n# Logging\n"})
	missing := filepath.Join(root, "does-not-exist")

	var out strings.Builder
	failed, err := prepareWorkspace(ctx, root, []string{rules, missing}, &out)
	if err != nil {
		t.Fatalf("prepareWorkspace: %v", err)
	}
	if len(failed) != 1 || failed[0] != missing {
		t.Fatalf("failed = %v, want only %s", failed, missing)
	}
	if names := attachedRepositories(ctx, root); len(names) != 1 || names[0] != "engineering" {
		t.Fatalf("attached = %v, want the good repository to have been indexed anyway", names)
	}
}

// TestSetupIndexesASingleRepositoryWithNoTargets is the team that has no
// separate rulebook — code and documents in one repository, which is
// most teams.
//
// `eng setup ~/code/my-app` used to register that repository and never
// index it, then report success: a working installation over an empty
// index, which answers every question with "nothing found" and looks
// exactly like a correct answer.
func TestSetupIndexesASingleRepositoryWithNoTargets(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	writeRepo(t, repo, map[string]string{
		"docs/error-handling.md": "---\ndoc: RULE\napplies_to: [\"**/*.go\"]\n---\n\n# Wrap errors\n",
		"src/main.go":            "package main\n",
	})

	var out strings.Builder
	if _, err := prepareWorkspace(ctx, repo, nil, &out); err != nil {
		t.Fatalf("prepareWorkspace: %v", err)
	}

	store, err := openStore(repo, false)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer store.Close()
	repos, err := store.ListRepositories(ctx)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repositories = %v (err %v), want exactly one", repos, err)
	}
	docs, err := store.ListDocuments(ctx, repos[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatalf("indexed 0 documents — setup reported success over an empty index\n%s", out.String())
	}

	// And the rule in it must be found, since that is the whole point.
	if n, counted := indexedRuleCount(ctx, repo); !counted || n != 1 {
		t.Errorf("indexedRuleCount = %d (counted %v), want 1", n, counted)
	}
}

// The closing advice is counted from the index, not inferred from
// whether --rules was passed. A team whose rules live alongside their
// code names no rulebook and has one.
func TestRuleCountIsMeasuredNotAssumed(t *testing.T) {
	ctx := context.Background()

	withRules := t.TempDir()
	writeRepo(t, withRules, map[string]string{
		"docs/logging.md": "---\ndoc: RULE\n---\n\n# Log with context\n",
	})
	var out strings.Builder
	if _, err := prepareWorkspace(ctx, withRules, nil, &out); err != nil {
		t.Fatal(err)
	}
	if n, counted := indexedRuleCount(ctx, withRules); !counted || n != 1 {
		t.Errorf("no --rules passed but rules present: got %d (counted %v), want 1", n, counted)
	}

	noRules := t.TempDir()
	writeRepo(t, noRules, map[string]string{"README.md": "# Just a readme\n"})
	if _, err := prepareWorkspace(ctx, noRules, nil, &out); err != nil {
		t.Fatal(err)
	}
	if n, counted := indexedRuleCount(ctx, noRules); !counted || n != 0 {
		t.Errorf("no rules anywhere: got %d (counted %v), want 0", n, counted)
	}

	// An unopenable workspace must report "could not count", not "none".
	if _, counted := indexedRuleCount(ctx, filepath.Join(t.TempDir(), "nothing-here")); counted {
		t.Error("reported a count for a workspace that does not exist")
	}
}

// Naming the same repository as both rulebook and code is what a
// single-repository team will do if they read the flags literally. It
// must not attach it twice or fail.
func TestSetupToleratesTheSameRepositoryTwice(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "my-app")
	writeRepo(t, repo, map[string]string{"docs/logging.md": "---\ndoc: RULE\n---\n\n# Log\n"})

	var out strings.Builder
	failed, err := prepareWorkspace(ctx, root, []string{repo, repo}, &out)
	if err != nil {
		t.Fatalf("prepareWorkspace: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %v, want none\n%s", failed, out.String())
	}
	if names := attachedRepositories(ctx, root); len(names) != 1 {
		t.Fatalf("attached = %v, want one entry", names)
	}
}

func TestIsRemote(t *testing.T) {
	remote := []string{
		"https://github.com/truelogics/engineering.git",
		"git@github.com:truelogics/engineering.git",
		"ssh://git@example.com/org/repo",
	}
	local := []string{
		".",
		"./engineering",
		"/Users/someone/code/engineering",
		"~/engineering-os/engineering",
		// A macOS path with a colon in a directory name is still a path.
		"/Users/someone/notes",
	}
	for _, target := range remote {
		if !isRemote(target) {
			t.Errorf("isRemote(%q) = false, want true", target)
		}
	}
	for _, target := range local {
		if isRemote(target) {
			t.Errorf("isRemote(%q) = true, want false", target)
		}
	}
}

func TestRepositoryNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/truelogics/engineering.git": "engineering",
		"https://github.com/truelogics/engineering":     "engineering",
		"git@github.com:truelogics/engineering.git":     "engineering",
		"https://example.com/org/repo/":                 "repo",
	}
	for url, want := range cases {
		if got := repositoryNameFromURL(url); got != want {
			t.Errorf("repositoryNameFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}

// writeRepo makes dir a git repository holding files. The .git marker is
// what tells setup whether the workspace root is a repository or a
// container, so it has to be real enough to stat.
func writeRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
