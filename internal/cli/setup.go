package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// mcpModule is where `engineering-mcp` is installed from when it is
// missing. Pinned to @latest rather than to a version: the transport and
// the kernel are released independently, and a version pinned in this
// file would go stale the first time either shipped without the other.
const mcpModule = "github.com/truelogics/engineering-mcp/cmd/engineering-mcp@latest"

// SetupOptions are the repositories `eng setup` should put in the
// workspace. Each entry is a local path or a git URL; URLs are cloned
// into the workspace directory first.
type SetupOptions struct {
	// Rules is the rulebook: whichever repository holds the
	// organization's rules, ADRs and standards. Separate from Repos only
	// so setup can tell the developer when they have not named one — a
	// workspace with no rulebook answers every question about rules with
	// a confident "none".
	Rules []string
	// Repos is the code to be reviewed.
	Repos []string
	// Force overwrites an existing /review-branch command.
	Force bool
}

// Setup implements `eng setup`: everything between a machine with the
// two binaries on it and a machine where `/review-branch` cites your
// organization's own decisions.
//
// It is orchestration and nothing else — workspace creation, attachment
// and indexing are the kernel's, the Claude Code registration is
// engineering-mcp's, and cloning is git's. What it contributes is the
// order, which is what the seven-step runbook was really encoding, and
// two decisions people got wrong by hand: that a directory holding
// several repositories should not itself be indexed, and that the
// registration must name absolute paths.
//
// Re-runnable. Running it again attaches what is new, re-indexes what is
// not, and repoints Claude Code at the current binary.
func Setup(ctx context.Context, dir string, out io.Writer, opts SetupOptions) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("setup: create %s: %w", absDir, err)
	}

	targets := append(append([]string{}, opts.Rules...), opts.Repos...)

	fmt.Fprintf(out, "Setting up Engineering OS in %s\n\n", absDir)

	// 1 and 2. The workspace and what goes in it.
	failed, err := prepareWorkspace(ctx, absDir, targets, out)
	if err != nil {
		return err
	}

	// 3. The transport.
	fmt.Fprintf(out, "\n[3/4] %s\n", mcpBinary)
	mcpPath, err := ensureMCP(ctx, out)
	if err != nil {
		return err
	}

	// 4. Claude Code, done by the component that owns it.
	fmt.Fprintf(out, "\n[4/4] Claude Code\n")
	args := []string{"install", "--workspace", absDir}
	if opts.Force {
		args = append(args, "--force")
	}
	cmd := exec.CommandContext(ctx, mcpPath, args...)
	cmd.Dir = absDir
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Its own report is already on screen. Wrapping it in "setup:
		// exit status 1" would bury the step that says what to fix.
		return errSilent{err}
	}

	if len(failed) > 0 {
		return fmt.Errorf("setup: %d repository/repositories could not be attached: %s",
			len(failed), strings.Join(failed, ", "))
	}
	printSetupNext(ctx, out, absDir)
	return nil
}

// prepareWorkspace creates the workspace and puts the named repositories
// in it, returning the targets that could not be attached.
//
// Separated from Setup so it can be tested without a transport binary or
// a network: the rest of Setup shells out to `go install` and
// `engineering-mcp install`, and the decisions worth testing — whether
// the root is a container, what a target resolves to — are all here.
func prepareWorkspace(ctx context.Context, absDir string, targets []string, out io.Writer) (failed []string, err error) {
	fmt.Fprintln(out, "[1/4] Workspace")
	created := false
	if _, statErr := os.Stat(dbPath(absDir)); statErr != nil {
		if err := Init(ctx, absDir, out); err != nil {
			return nil, err
		}
		created = true
	} else {
		fmt.Fprintf(out, "Already a workspace: %s\n", dbPath(absDir))
	}

	// A directory holding several repositories is a container, not a
	// repository, and indexing it as one indexes every child twice —
	// once under its own name and once under the root's. Citations then
	// read `engineering-os:rules/logging.md` for a file that belongs to
	// `engineering`, and two repositories become indistinguishable.
	//
	// Decided from the filesystem rather than asked: a directory that is
	// not itself a git repository, and that is about to have
	// repositories attached inside it, is a container. The install guide
	// spelled this out as "run detach immediately after create", a step
	// whose reason nobody could see from where it was written.
	if created && len(targets) > 0 && !isGitRepository(absDir) {
		if err := WorkspaceDetach(ctx, absDir, absDir, out); err != nil {
			return nil, err
		}
		fmt.Fprintln(out, "(the workspace root is a container for the repositories below, so it is not indexed itself)")
	}

	fmt.Fprintf(out, "\n[2/4] Repositories\n")

	// Pointed straight at a repository, with nothing named: that
	// repository is the workspace, and it is what to index.
	//
	// It was previously left registered and unindexed, and setup went on
	// to report success — `eng setup ~/code/my-app` produced a working
	// installation over an empty index, which answers every question with
	// "nothing found". Indexing only ever happened inside the loop below,
	// which no target entered. The single-repository team, who has no
	// separate rulebook to name, is exactly who hits this.
	if len(targets) == 0 && isGitRepository(absDir) {
		fmt.Fprintln(out, "None named, so indexing this repository itself.")
		if err := WorkspaceAttach(ctx, absDir, absDir, out); err != nil {
			return nil, err
		}
	}
	if len(targets) == 0 && !isGitRepository(absDir) {
		fmt.Fprintln(out, "None named, and this directory is not a git repository, so there is")
		fmt.Fprintln(out, "nothing to index yet. Add one with:")
		fmt.Fprintf(out, "    eng setup %s --repo /path/to/your-repository\n", absDir)
	}

	for _, target := range targets {
		path, err := obtain(ctx, absDir, target, out)
		if err != nil {
			fmt.Fprintf(out, "%s: FAILED — %v\n", target, err)
			failed = append(failed, target)
			continue
		}
		// One repository must not take the setup down with it: a
		// mistyped path should not stop the rulebook that was named
		// alongside it from being indexed.
		if err := WorkspaceAttach(ctx, absDir, path, out); err != nil {
			fmt.Fprintf(out, "%s: FAILED — %v\n", target, err)
			failed = append(failed, target)
		}
	}
	return failed, nil
}

// obtain resolves one target to a local directory, cloning it first when
// it is a URL. An existing clone is reused rather than re-cloned, so
// re-running setup is not destructive.
func obtain(ctx context.Context, workspaceDir, target string, out io.Writer) (string, error) {
	if !isRemote(target) {
		abs, err := filepath.Abs(target)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory (and does not look like a git URL)", abs)
		}
		return abs, nil
	}

	dest := filepath.Join(workspaceDir, repositoryNameFromURL(target))
	if _, err := os.Stat(dest); err == nil {
		fmt.Fprintf(out, "Using existing clone: %s\n", dest)
		return dest, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git is not on $PATH, so %s cannot be cloned", target)
	}
	fmt.Fprintf(out, "Cloning %s -> %s\n", target, dest)
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", target, dest)
	cmd.Dir = workspaceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return dest, nil
}

// isRemote distinguishes a git URL from a path. Both SSH shorthand
// (git@host:org/repo.git) and full URLs count; anything else is treated
// as a local directory and checked as one, so a typo'd path fails with
// "is not a directory" rather than being handed to git.
func isRemote(target string) bool {
	if strings.Contains(target, "://") {
		return true
	}
	at := strings.Index(target, "@")
	colon := strings.Index(target, ":")
	return at > 0 && colon > at
}

// repositoryNameFromURL is the directory git clone would create.
func repositoryNameFromURL(url string) string {
	name := strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	if i := strings.LastIndexAny(name, "/:"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return "repository"
	}
	return name
}

// ensureMCP finds engineering-mcp, installing it if it is absent.
//
// `go install` rather than a build from a clone: setup has no way to
// know where the source is, and requiring one would put the clone back
// into an install this command exists to remove. When the install
// succeeds and the binary is still not on $PATH, that is GOBIN not being
// on it — reported as such, because "command not found" immediately
// after a successful install is otherwise unreadable.
func ensureMCP(ctx context.Context, out io.Writer) (string, error) {
	if path, err := exec.LookPath(mcpBinary); err == nil {
		fmt.Fprintf(out, "Already installed: %s\n", path)
		return path, nil
	}

	if _, err := exec.LookPath("go"); err != nil {
		return "", fmt.Errorf("setup: %s is not installed and the go toolchain is not on $PATH to install it.\n"+
			"    Install Go, or build it yourself: go build -o ~/.local/bin/%s ./cmd/%s", mcpBinary, mcpBinary, mcpBinary)
	}

	fmt.Fprintf(out, "Not installed. Running: go install %s\n", mcpModule)
	cmd := exec.CommandContext(ctx, "go", "install", mcpModule)
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("setup: go install %s: %w", mcpModule, err)
	}

	path, err := exec.LookPath(mcpBinary)
	if err != nil {
		return "", fmt.Errorf("setup: %s installed to %s, which is not on $PATH.\n"+
			"    Add it, in your shell profile rather than this session:\n"+
			"        export PATH=\"%s:$PATH\"\n"+
			"    then run `eng setup` again.", mcpBinary, goBin(), goBin())
	}
	fmt.Fprintf(out, "Installed: %s\n", path)
	return path, nil
}

// goBin is where `go install` puts binaries: $GOBIN, else $GOPATH/bin.
// Asked of the toolchain rather than assumed, because advising a
// developer to add the wrong directory to $PATH wastes more time than
// saying nothing would.
func goBin() string {
	for _, v := range []string{"GOBIN", "GOPATH"} {
		out, err := exec.Command("go", "env", v).Output()
		if err != nil {
			continue
		}
		if dir := strings.TrimSpace(string(out)); dir != "" {
			if v == "GOPATH" {
				return filepath.Join(dir, "bin")
			}
			return dir
		}
	}
	return filepath.Join(os.Getenv("HOME"), "go", "bin")
}

// indexedRuleCount reports how many rules the workspace actually holds.
// Best-effort: counted reports whether the question could be answered at
// all, so a store that will not open is not reported as "no rules".
func indexedRuleCount(ctx context.Context, absDir string) (count int, counted bool) {
	store, err := openStore(absDir, false)
	if err != nil {
		return 0, false
	}
	defer store.Close()
	rules, err := store.ListDocumentsByType(ctx, domain.DocTypeRule)
	if err != nil {
		return 0, false
	}
	return len(rules), true
}

func isGitRepository(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func printSetupNext(ctx context.Context, out io.Writer, absDir string) {
	fmt.Fprintln(out, "\nDone.")

	// Counted, not inferred from whether --rules was passed. A team whose
	// rules live in the same repository as their code names no rulebook
	// and has one; a team that named a repository holding no rules has
	// none. Only the index knows which, and the failure it decides is
	// silent — retrieval succeeds, finds nothing, and reports that no
	// rule governs the change, which reads exactly like a correct answer.
	rules, counted := indexedRuleCount(ctx, absDir)
	switch {
	case counted && rules > 0:
		fmt.Fprintf(out, "\n%d rule(s) indexed. Reviews here can cite them.\n", rules)
	case counted:
		fmt.Fprintln(out, "\nNo rules were found in what you indexed, so reviews here will be told that")
		fmt.Fprintln(out, "no engineering rule governs anything — indistinguishable from a correct")
		fmt.Fprintln(out, "answer. If your team's rules live somewhere else, add that repository:")
		fmt.Fprintf(out, "    eng setup %s --rules /path/to/your-rules-repo\n", absDir)
		fmt.Fprintln(out, "\nIf they live in what you just indexed, they are probably not recognised as")
		fmt.Fprintln(out, "rules yet. Run `eng taxonomy auto` in that repository — it proposes what")
		fmt.Fprintln(out, "your directories mean, and asks before writing anything.")
	}
	fmt.Fprintln(out, "\nNext:")
	fmt.Fprintln(out, "    cd /path/to/your-application")
	fmt.Fprintln(out, "    eng doctor        # check the whole chain")
	fmt.Fprintln(out, "    claude            # then type: /review-branch")
	fmt.Fprintln(out, "\nAfter your documents change: eng update "+absDir)
}
