package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

// TestDefinitionOfDone exercises exactly the command sequence Step 7's
// Definition of Done specifies: init, index ., search (twice), status —
// on a directory containing markdown documentation.
func TestDefinitionOfDone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "---\ndoc: README\nstatus: living\n---\n\n# Demo Project\n\nWe chose JWT for stateless authentication across services.\n")
	writeFile(t, dir, "docs/ARCHITECTURE.md", "# Architecture\n\nThis document describes the pipeline architecture of the system.\n")

	var out bytes.Buffer

	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !strings.Contains(out.String(), "Created workspace") {
		t.Errorf("Init output = %q, want it to mention workspace creation", out.String())
	}
	if _, err := os.Stat(dbPath(dir)); err != nil {
		t.Fatalf("expected %s to exist after Init: %v", dbPath(dir), err)
	}

	out.Reset()
	if err := Index(ctx, dir, &out); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !strings.Contains(out.String(), "2 scanned") || !strings.Contains(out.String(), "2 added") {
		t.Errorf("Index output = %q, want 2 scanned, 2 added", out.String())
	}

	out.Reset()
	if err := Search(ctx, dir, "architecture", &out); err != nil {
		t.Fatalf("Search(architecture): %v", err)
	}
	if !strings.Contains(out.String(), "ARCHITECTURE.md") {
		t.Errorf("Search(architecture) output = %q, want a hit on ARCHITECTURE.md", out.String())
	}

	out.Reset()
	if err := Search(ctx, dir, "authentication", &out); err != nil {
		t.Fatalf("Search(authentication): %v", err)
	}
	if !strings.Contains(out.String(), "README.md") {
		t.Errorf("Search(authentication) output = %q, want a hit on README.md", out.String())
	}

	out.Reset()
	if err := Status(ctx, dir, &out); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(out.String(), "clean") {
		t.Errorf("Status output = %q, want status 'clean'", out.String())
	}
}

func TestIndexWithoutInitFails(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Index(context.Background(), dir, &out); err == nil {
		t.Fatal("Index: expected error when no workspace has been initialized")
	}
}

func TestSearchWithoutInitFails(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Search(context.Background(), dir, "anything", &out); err == nil {
		t.Fatal("Search: expected error when no workspace has been initialized")
	}
}

func TestStatusWithoutInitFails(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Status(context.Background(), dir, &out); err == nil {
		t.Fatal("Status: expected error when no workspace has been initialized")
	}
}

func TestStatusWithNoRepositoriesRegistered(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Init itself registers dir as a repository, so status should show
	// one entry even before any index run.
	out.Reset()
	if err := Status(ctx, dir, &out); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(out.String(), "not indexed") {
		t.Errorf("Status output = %q, want 'not indexed' before any eng index run", out.String())
	}
}

func TestInitIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var out bytes.Buffer

	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init (first): %v", err)
	}
	out.Reset()
	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init (second): %v", err)
	}
	if !strings.Contains(out.String(), "Already registered") {
		t.Errorf("second Init output = %q, want 'Already registered'", out.String())
	}
}

func TestSearchNoMatches(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Hello\n\nNothing relevant here.\n")
	var out bytes.Buffer
	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := Index(ctx, dir, &out); err != nil {
		t.Fatalf("Index: %v", err)
	}
	out.Reset()
	if err := Search(ctx, dir, "zzzznonexistent", &out); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(out.String(), "No matches") {
		t.Errorf("Search output = %q, want 'No matches'", out.String())
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestSyncEndToEnd exercises `eng sync` through the same real-CLI path
// TestDefinitionOfDone uses for the other four commands — Milestone 3.
func TestSyncEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Demo\n\nOriginal content.\n")
	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial")

	var out bytes.Buffer
	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init: %v", err)
	}
	out.Reset()
	if err := Index(ctx, dir, &out); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !strings.Contains(out.String(), "1 added") {
		t.Fatalf("Index output = %q, want 1 added", out.String())
	}

	writeFile(t, dir, "README.md", "# Demo\n\nUpdated content about authentication.\n")
	writeFile(t, dir, "NEW.md", "# New\n")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "second")

	out.Reset()
	if err := Sync(ctx, dir, &out); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !strings.Contains(out.String(), "1 added") || !strings.Contains(out.String(), "1 updated") {
		t.Fatalf("Sync output = %q, want 1 added and 1 updated", out.String())
	}

	out.Reset()
	if err := Search(ctx, dir, "authentication", &out); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(out.String(), "README.md") {
		t.Fatalf("Search(authentication) after Sync = %q, want a hit on README.md", out.String())
	}
}

func TestSyncWithoutInitFails(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Sync(context.Background(), dir, &out); err == nil {
		t.Fatal("Sync: expected error when no workspace has been initialized")
	}
}

// TestContextEndToEnd exercises `eng context`/`eng ask`'s full pipeline —
// Milestone 6 — matching Step 8's Definition of Done shape.
func TestContextEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "ARCHITECTURE.md", "---\ndoc: ARCHITECTURE\n---\n\n# Architecture\n\nThe authentication pipeline.\n")
	writeFile(t, dir, "engineering/ADR/0003-jwt.md", "# ADR 0003\n\nAuthentication decision: use JWT.\n")

	var out bytes.Buffer
	if err := Init(ctx, dir, &out); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := Index(ctx, dir, &out); err != nil {
		t.Fatalf("Index: %v", err)
	}

	out.Reset()
	if err := Context(ctx, dir, "Review authentication PR", &out); err != nil {
		t.Fatalf("Context: unexpected error: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "Architecture:") || !strings.Contains(body, "ARCHITECTURE.md") {
		t.Errorf("Context output = %q, want an Architecture section with ARCHITECTURE.md", body)
	}
	if !strings.Contains(body, "Related ADRs:") || !strings.Contains(body, "0003-jwt.md") {
		t.Errorf("Context output = %q, want a Related ADRs section with the ADR", body)
	}
	if !strings.Contains(body, "Related Issues:") || !strings.Contains(body, "none indexed yet") {
		t.Errorf("Context output = %q, want an empty Related Issues section, not omitted", body)
	}
}

func TestContextWithoutInitFails(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := Context(context.Background(), dir, "anything", &out); err == nil {
		t.Fatal("Context: expected error when no workspace has been initialized")
	}
}

// TestWorkspaceAttachMakesAnotherRepositoryRetrievable is the Sprint 7
// end-to-end case: a workspace created in one directory, with a second
// repository attached, must retrieve the second repository's rules for a
// task about the first. Before `eng workspace attach` existed this was
// reachable only from Go, so in practice every workspace held exactly
// one repository and a review could never see the organization's rules.
func TestWorkspaceAttachMakesAnotherRepositoryRetrievable(t *testing.T) {
	ctx := context.Background()

	app := t.TempDir()
	writeFile(t, app, "README.md", "---\ndoc: README\n---\n\n# App\n\nThe billing service writes invoices.\n")

	knowledge := t.TempDir()
	writeFile(t, knowledge, "rules/no-raw-sql.md",
		"---\ndoc: RULE\nid: no-raw-sql\n---\n\n# Rule: invoices are written through the store\n\nBilling code never writes raw SQL for invoices.\n")

	var out strings.Builder
	if err := WorkspaceCreate(ctx, app, &out); err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
	}
	if err := WorkspaceAttach(ctx, app, knowledge, &out); err != nil {
		t.Fatalf("WorkspaceAttach: %v", err)
	}
	if err := Index(ctx, app, &out); err != nil {
		t.Fatalf("Index: %v", err)
	}

	var ctxOut strings.Builder
	if err := Context(ctx, app, "invoices billing raw SQL store", &ctxOut); err != nil {
		t.Fatalf("Context: %v", err)
	}
	if !strings.Contains(ctxOut.String(), "rules/no-raw-sql.md") {
		t.Fatalf("context did not retrieve the attached repository's rule:\n%s", ctxOut.String())
	}

	// Detach removes it again, documents and all.
	var detachOut strings.Builder
	if err := WorkspaceDetach(ctx, app, knowledge, &detachOut); err != nil {
		t.Fatalf("WorkspaceDetach: %v", err)
	}
	var afterOut strings.Builder
	if err := Context(ctx, app, "invoices billing raw SQL store", &afterOut); err != nil {
		t.Fatalf("Context after detach: %v", err)
	}
	if strings.Contains(afterOut.String(), "no-raw-sql") {
		t.Errorf("detached repository's documents still retrievable:\n%s", afterOut.String())
	}
}

// TestSyncCoversTheWholeWorkspace is the install documentation's own
// refresh step, as a test.
//
// `eng sync` used to sync the workspace directory alone, registering it
// as a repository when it wasn't one. On the layout the documentation
// recommends — a workspace root that is the *parent* of several
// repositories, detached on purpose so citations stay distinguishable —
// that meant sync skipped every attached repository, silently undid the
// detach, and indexed all their documents a second time under the root's
// name. Found on a clean machine by following INSTALL.md.
func TestSyncCoversTheWholeWorkspace(t *testing.T) {
	ctx := context.Background()

	root := t.TempDir()
	app := filepath.Join(root, "app")
	knowledge := filepath.Join(root, "knowledge")
	for _, d := range []string{app, knowledge} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, app, "README.md", "---\ndoc: README\n---\n\n# App\n\nThe billing service writes invoices.\n")
	writeFile(t, knowledge, "rules/no-raw-sql.md",
		"---\ndoc: RULE\nid: no-raw-sql\n---\n\n# Rule\n\nBilling code never writes raw SQL for invoices.\n")

	var out strings.Builder
	if err := WorkspaceCreate(ctx, root, &out); err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
	}
	// What the documentation tells the reader to do, and the reason the
	// bug was invisible: the root is a container, not a repository.
	if err := WorkspaceDetach(ctx, root, root, &out); err != nil {
		t.Fatalf("WorkspaceDetach: %v", err)
	}
	if err := WorkspaceAttach(ctx, root, app, &out); err != nil {
		t.Fatalf("WorkspaceAttach app: %v", err)
	}
	if err := WorkspaceAttach(ctx, root, knowledge, &out); err != nil {
		t.Fatalf("WorkspaceAttach knowledge: %v", err)
	}

	var syncOut strings.Builder
	if err := Sync(ctx, root, &syncOut); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for _, want := range []string{"app:", "knowledge:"} {
		if !strings.Contains(syncOut.String(), want) {
			t.Errorf("sync did not cover %s — it must re-index every attached repository:\n%s", want, syncOut.String())
		}
	}

	var statusOut strings.Builder
	if err := Status(ctx, root, &statusOut); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if strings.Contains(statusOut.String(), filepath.Base(root)) {
		t.Errorf("sync re-attached the workspace root, duplicating every document under one name:\n%s", statusOut.String())
	}
}

func TestWorkspaceDetachUnknownRepositoryFails(t *testing.T) {
	dir := t.TempDir()
	var out strings.Builder
	if err := WorkspaceCreate(context.Background(), dir, &out); err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
	}
	if err := WorkspaceDetach(context.Background(), dir, t.TempDir(), &out); err == nil {
		t.Fatal("WorkspaceDetach: want an error for a repository that was never attached")
	}
}
