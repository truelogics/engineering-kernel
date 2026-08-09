// This file holds the commands RFC-0008 adds to make `eng` the shell of
// the Engineering OS rather than the CLI of one component.
//
// They are orchestration and presentation. Every one of them delegates:
// classification to domain.ParseTaxonomy, indexing to the Indexer,
// diagnostics to engineering-mcp, retrieval to the retriever. Where a
// command here starts to want logic of its own, that is the signal the
// capability does not exist in the kernel yet — and adding it here would
// hide that.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/truelogics/ai-memory/internal/domain"
	"github.com/truelogics/ai-memory/internal/kernel"
)

// mcpBinary is the transport's command name. `eng` shells out to it
// rather than reimplementing what it knows: whether Claude Code is
// registered, and whether the server it registered actually starts, are
// facts about MCP, and the kernel has no business learning them.
const mcpBinary = "engineering-mcp"

// Update implements `eng update`: the incremental re-index, under the
// name a developer reaches for when they have changed some documents and
// want the index to catch up. Same operation as `eng sync`.
func Update(ctx context.Context, dir string, out io.Writer) error {
	return Sync(ctx, dir, out)
}

// Doctor implements `eng doctor` by running `engineering-mcp doctor`,
// which already answers all eight questions including the four about MCP
// and Claude Code.
//
// Delegation rather than reimplementation, and not only to avoid
// duplication: a second set of checks would drift from the first, and the
// two would disagree about a machine neither could fix. When the transport
// is not installed, that is itself the diagnosis, and the workspace facts
// `eng` can answer alone are reported so the developer is not left with
// nothing.
func Doctor(ctx context.Context, dir string, out io.Writer, args []string) error {
	path, err := exec.LookPath(mcpBinary)
	if err != nil {
		fmt.Fprintf(out, "%s is not installed, so the MCP and Claude Code checks cannot run.\n\n", mcpBinary)
		fmt.Fprintf(out, "To install it:\n    go build -o ~/.local/bin/%s ./cmd/%s      # in the engineering-mcp repository\n\n", mcpBinary, mcpBinary)
		fmt.Fprintln(out, "What this workspace can report on its own:")
		fmt.Fprintln(out)
		// Resolved upward, not taken as given. The useful layout puts the
		// workspace above several repositories, so reporting on the
		// current directory would say "no workspace here" from inside a
		// perfectly indexed repository.
		absDir, absErr := filepath.Abs(dir)
		if absErr == nil {
			if workspaceDir, ok := findWorkspaceUpward(absDir); ok {
				dir = workspaceDir
			}
		}
		if err := WorkspaceStatus(ctx, dir, out); err != nil {
			fmt.Fprintf(out, "  %v\n", err)
		}
		return fmt.Errorf("doctor: %s is not on $PATH", mcpBinary)
	}

	cmd := exec.CommandContext(ctx, path, append([]string{"doctor"}, args...)...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// engineering-mcp doctor exits non-zero when a check fails. That
		// is its report, not a failure to produce one, and wrapping it in
		// "eng: doctor: exit status 1" would bury the eight lines above
		// that say what is actually wrong.
		return errSilent{err}
	}
	return nil
}

// errSilent carries an exit status without a message, for a subcommand
// that has already said everything worth saying on its own output.
type errSilent struct{ err error }

func (e errSilent) Error() string { return e.err.Error() }
func (e errSilent) Silent() bool  { return true }

// Unwrap keeps errors.Is and errors.As working through this wrapper.
// Both producers wrap exec.CommandContext, so a cancelled context
// arrives here as context.Canceled and a caller must still be able to
// see it — engineering:rules/go-wrap-errors.md asks for %w precisely so
// wrapping does not sever the chain.
func (e errSilent) Unwrap() error { return e.err }

// Silent reports whether err was produced by a command that has already
// printed its own diagnosis, so main can exit non-zero without printing a
// second, less useful one.
//
// errors.As rather than a type assertion, so a wrapper added above this
// one later does not silently turn the answer back to false.
func Silent(err error) bool {
	var s interface{ Silent() bool }
	return errors.As(err, &s) && s.Silent()
}

// Clean implements `eng clean`: removes the generated .eng/ directory.
//
// Destructive and irreversible — the index is rebuilt from documents, but
// the list of *which repositories were attached* is not stored anywhere
// else, so cleaning a multi-repository workspace loses the setup rather
// than the data. It therefore names what it will destroy and refuses
// without confirmation.
func Clean(ctx context.Context, dir string, out io.Writer, confirmed bool) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("clean: %w", err)
	}
	target := filepath.Join(absDir, dbDirName)
	if _, err := os.Stat(target); err != nil {
		fmt.Fprintf(out, "Nothing to clean: %s does not exist.\n", target)
		return nil
	}

	attached := attachedRepositories(ctx, absDir)
	if !confirmed {
		fmt.Fprintf(out, "This will delete %s.\n\n", target)
		if len(attached) > 0 {
			fmt.Fprintf(out, "%d repositories are attached to this workspace:\n", len(attached))
			for _, name := range attached {
				fmt.Fprintf(out, "  %s\n", name)
			}
			fmt.Fprintln(out, "\nThe index rebuilds from your documents. The list of attachments does not —")
			fmt.Fprintln(out, "you will have to run `eng workspace attach` for each of them again.")
		}
		fmt.Fprintln(out, "\nRe-run with --yes to proceed.")
		return fmt.Errorf("clean: not confirmed")
	}

	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("clean: remove %s: %w", target, err)
	}
	fmt.Fprintf(out, "Removed %s\n", target)
	if len(attached) > 0 {
		fmt.Fprintf(out, "%d attachments were lost. Re-create with `eng workspace create .` and `eng workspace attach <path>`.\n", len(attached))
	}
	return nil
}

// attachedRepositories names what is registered, best-effort: `eng clean`
// must work on a corrupt workspace, which is one of the reasons to run it.
func attachedRepositories(ctx context.Context, absDir string) []string {
	store, err := openStore(absDir, false)
	if err != nil {
		return nil
	}
	defer store.Close()
	repos, err := store.ListRepositories(ctx)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	return names
}

// WorkspaceStatus implements `eng workspace status`: what this workspace
// is, what is in it, and what it can answer.
//
// The last line is the one that matters. A workspace holding an
// application and no rulebook answers every question about rules with a
// confident "none", and that answer is indistinguishable from a correct
// one — so the count of rules is reported even when it is zero, and
// especially when it is zero.
func WorkspaceStatus(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("workspace status: %w", err)
	}
	store, err := openStore(absDir, false)
	if err != nil {
		return fmt.Errorf("workspace status: %w", err)
	}
	defer store.Close()

	repos, err := store.ListRepositories(ctx)
	if err != nil {
		return fmt.Errorf("workspace status: %w", err)
	}

	fmt.Fprintf(out, "Workspace:    %s\n", dbPath(absDir))
	fmt.Fprintf(out, "Repositories: %d\n", len(repos))

	total := 0
	for _, repo := range repos {
		docs, err := store.ListDocuments(ctx, repo.ID)
		if err != nil {
			return fmt.Errorf("workspace status: %w", err)
		}
		total += len(docs)
	}
	fmt.Fprintf(out, "Documents:    %d\n", total)

	rules, err := store.ListDocumentsByType(ctx, domain.DocTypeRule)
	if err != nil {
		return fmt.Errorf("workspace status: %w", err)
	}
	fmt.Fprintf(out, "Rules:        %d\n", len(rules))
	if len(rules) == 0 {
		fmt.Fprintln(out, "\nNo rules are indexed here. Every review against this workspace will be told")
		fmt.Fprintln(out, "that no engineering rule governs anything, which reads exactly like a correct")
		fmt.Fprintln(out, "answer. Attach the repository holding your rules:")
		fmt.Fprintf(out, "    cd %s && eng workspace attach /path/to/your-rules-repo\n", absDir)
	}
	return nil
}

// statusHeader prints what `eng status` used to leave the reader to work
// out: which workspace answered, whether this repository declares a
// taxonomy, and whether a rulebook is present at all.
//
// The rule count is the load-bearing line. A workspace holding no rules
// answers every question about them with a confident "none", and that is
// indistinguishable from a correct answer unless something says so.
func statusHeader(ctx context.Context, store kernel.Storage, absDir string, out io.Writer) error {
	fmt.Fprintf(out, "Workspace: %s\n", dbPath(absDir))

	taxonomy := filepath.Join(absDir, domain.TaxonomyFile)
	if _, err := os.Stat(taxonomy); err == nil {
		fmt.Fprintf(out, "Taxonomy:  %s\n", domain.TaxonomyFile)
	} else {
		fmt.Fprintf(out, "Taxonomy:  none here — `eng taxonomy` explains what one does\n")
	}

	rules, err := store.ListDocumentsByType(ctx, domain.DocTypeRule)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	fmt.Fprintf(out, "Rules:     %d indexed", len(rules))
	if len(rules) == 0 {
		fmt.Fprint(out, "  — every review here will be told nothing governs these files")
	}
	fmt.Fprintln(out)

	unknown, err := store.ListDocumentsByType(ctx, domain.DocTypeUnknown)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if len(unknown) > 0 {
		fmt.Fprintf(out, "Unclassified: %d document(s) — retrievable only as keyword hits\n", len(unknown))
	}
	fmt.Fprintln(out)
	return nil
}

// Taxonomy implements `eng taxonomy`: what this repository has declared
// its directories mean (RFC-0007), or how to say so when it has not.
func Taxonomy(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("taxonomy: %w", err)
	}
	path := filepath.Join(absDir, domain.TaxonomyFile)

	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		printNoTaxonomy(out, path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("taxonomy: read %s: %w", path, err)
	}

	tax, err := domain.ParseTaxonomy(content)
	if err != nil {
		return fmt.Errorf("taxonomy: %w", err)
	}

	fmt.Fprintf(out, "%s\n\n", path)
	if tax.Empty() {
		fmt.Fprintln(out, "The file exists and declares no mappings.")
		return nil
	}
	for _, m := range tax.Mappings() {
		fmt.Fprintf(out, "  %-28s -> %s\n", m.Pattern, m.Declared)
	}
	fmt.Fprintln(out, "\nListed most specific first, which is the order they are consulted in.")
	fmt.Fprintln(out, "\nA document's own `doc:` front matter always wins over these.")
	return nil
}

func printNoTaxonomy(out io.Writer, path string) {
	fmt.Fprintf(out, "No %s in this repository.\n\n", domain.TaxonomyFile)
	fmt.Fprintln(out, "Without one, only documents carrying `doc:` front matter are classified,")
	fmt.Fprintln(out, "and everything else is retrievable only as a keyword hit. On the first")
	fmt.Fprintln(out, "repository this was measured on, that was 91% of the documents.")
	fmt.Fprintf(out, "\nTo say what your directories mean, write %s:\n\n", path)
	fmt.Fprintln(out, "    taxonomy:")
	fmt.Fprintln(out, "      plans/**:      Decision")
	fmt.Fprintln(out, "      handbook/**:   Guide")
	fmt.Fprintln(out, "      product/**:    Specification")
	fmt.Fprintln(out, "\nCanonical types: Rule, Decision, Architecture, Specification, Guide,")
	fmt.Fprintln(out, "Planning, Reference, Other. The longest matching pattern wins.")
	fmt.Fprintln(out, "\nWrite it yourself. It is a claim about what your directories mean, and a")
	fmt.Fprintln(out, "wrong mapping returns confidently mislabelled documents — worse than none.")
	fmt.Fprintln(out, "\nThen: eng index <workspace>")
}

// TaxonomyValidate implements `eng taxonomy validate`: parses the file
// and, when the repository is indexed, reports what it actually achieves.
//
// Parsing alone is a weak check. A taxonomy can be perfectly well-formed
// and match nothing, which looks identical to not having written one —
// so this counts the indexed documents each pattern would claim, and says
// how many remain unclassified.
func TaxonomyValidate(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("taxonomy validate: %w", err)
	}
	path := filepath.Join(absDir, domain.TaxonomyFile)

	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("taxonomy validate: no %s in %s", domain.TaxonomyFile, absDir)
	}
	if err != nil {
		return fmt.Errorf("taxonomy validate: read %s: %w", path, err)
	}

	tax, err := domain.ParseTaxonomy(content)
	if err != nil {
		return fmt.Errorf("taxonomy validate: %w", err)
	}
	fmt.Fprintf(out, "%s parses.\n", path)
	if tax.Empty() {
		fmt.Fprintln(out, "It declares no mappings, so it classifies nothing.")
		return nil
	}

	docs, workspaceDir, err := indexedDocumentsFor(ctx, absDir)
	if err != nil {
		fmt.Fprintf(out, "\n%d mapping(s) declared. Not checked against real documents: %v\n", len(tax.Mappings()), err)
		fmt.Fprintln(out, "Index the workspace and run this again to see what they actually claim.")
		return nil
	}

	// Only documents this file actually decided count as decided by it.
	//
	// A first version credited any already-classified document that also
	// matched a pattern, which is wrong twice over: applyTaxonomy touches
	// only `unknown` documents, so front matter had made that decision;
	// and a pattern contradicted by front matter is not a success but a
	// disagreement about what a directory holds. Reporting it as a clean
	// bill of health is exactly the failure this command exists to catch.
	var decided, wouldDecide, unclassified int
	var contradicted []string
	for _, doc := range docs {
		declared, claims := tax.TypeFor(doc.Path)
		switch {
		case doc.Type == domain.DocTypeUnknown && claims:
			wouldDecide++
		case doc.Type == domain.DocTypeUnknown:
			unclassified++
		case claims && doc.Type == declared:
			decided++ // consistent: front matter agrees, or this file set it
		case claims:
			contradicted = append(contradicted, fmt.Sprintf("%s is %s, this file calls it %s", doc.Path, doc.Type, declared))
		}
	}

	fmt.Fprintf(out, "\nAgainst the index at %s — %d document(s) in this repository:\n", workspaceDir, len(docs))
	fmt.Fprintf(out, "  %d classified consistently with this file\n", decided)
	fmt.Fprintf(out, "  %d still unknown and claimed by this file — applied on the next `eng index`\n", wouldDecide)
	fmt.Fprintf(out, "  %d still unknown and unclaimed\n", unclassified)

	if len(contradicted) > 0 {
		fmt.Fprintf(out, "\n%d document(s) contradict this file. Front matter wins, so these patterns\n", len(contradicted))
		fmt.Fprintln(out, "have no effect on them — and the disagreement is worth resolving:")
		for i, c := range contradicted {
			if i == 5 {
				fmt.Fprintf(out, "  ... and %d more\n", len(contradicted)-5)
				break
			}
			fmt.Fprintf(out, "  %s\n", c)
		}
	}

	switch {
	case decided == 0 && wouldDecide == 0 && len(contradicted) == 0 && unclassified > 0:
		fmt.Fprintln(out, "\nThe file is valid and matches nothing, which is indistinguishable from not")
		fmt.Fprintln(out, "having written one. Check the patterns against real paths — `**` crosses")
		fmt.Fprintln(out, "directory separators and `*` does not.")
	case unclassified == 0 && wouldDecide == 0 && len(contradicted) == 0:
		fmt.Fprintln(out, "\nEvery document here is classified. Nothing left for this file to do.")
	case unclassified > 0:
		fmt.Fprintf(out, "\n`eng status` in %s lists what is still unknown across the workspace.\n", workspaceDir)
	}
	return nil
}

// indexedDocumentsFor returns dir's indexed documents by finding the
// workspace that holds them, which is not necessarily dir: the useful
// layout puts the workspace above several repositories.
func indexedDocumentsFor(ctx context.Context, absDir string) ([]domain.CanonicalDocument, string, error) {
	workspaceDir, ok := findWorkspaceUpward(absDir)
	if !ok {
		return nil, "", fmt.Errorf("no indexed workspace at or above %s", absDir)
	}
	store, err := openStore(workspaceDir, false)
	if err != nil {
		return nil, "", err
	}
	defer store.Close()

	repo, found, err := store.FindRepositoryByPath(ctx, absDir)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "", fmt.Errorf("%s is not attached to the workspace at %s", absDir, workspaceDir)
	}
	docs, err := store.ListDocuments(ctx, repo.ID)
	if err != nil {
		return nil, "", err
	}
	return docs, workspaceDir, nil
}

// findWorkspaceUpward mirrors the resolution engineering-mcp performs, so
// `eng` and the server agree about which workspace answers for a
// directory. The nearest one wins.
func findWorkspaceUpward(dir string) (string, bool) {
	for {
		if info, err := os.Stat(dbPath(dir)); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Config implements `eng config`: the effective configuration, meaning
// what this directory resolves to right now rather than what any file
// says. Every line is a fact that changes what a review sees.
func Config(ctx context.Context, dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	fmt.Fprintf(out, "%-22s %s\n", "working directory", absDir)

	workspaceDir, ok := findWorkspaceUpward(absDir)
	if ok {
		fmt.Fprintf(out, "%-22s %s\n", "workspace", workspaceDir)
		fmt.Fprintf(out, "%-22s %s\n", "index", dbPath(workspaceDir))
	} else {
		fmt.Fprintf(out, "%-22s %s\n", "workspace", "none at or above this directory")
	}

	// Named even though eng does not read it: it is what the registered
	// MCP server falls back to, so a developer comparing eng's answer with
	// a review's needs to see both.
	env := strings.TrimSpace(os.Getenv("ENGINEERING_WORKSPACE"))
	if env == "" {
		env = "(unset)"
	}
	fmt.Fprintf(out, "%-22s %s   (used by %s, not by eng)\n", "ENGINEERING_WORKSPACE", env, mcpBinary)

	taxonomy := filepath.Join(absDir, domain.TaxonomyFile)
	if _, err := os.Stat(taxonomy); err == nil {
		fmt.Fprintf(out, "%-22s %s\n", "taxonomy", taxonomy)
	} else {
		fmt.Fprintf(out, "%-22s %s\n", "taxonomy", "none — run `eng taxonomy`")
	}

	for _, bin := range []string{mcpBinary, "claude", "git"} {
		path, err := exec.LookPath(bin)
		if err != nil {
			path = "not on $PATH"
		}
		fmt.Fprintf(out, "%-22s %s\n", bin, path)
	}
	return nil
}

// Review implements `eng review`. It does not review anything.
//
// It checks the three things that make a review able to cite the
// organization — a repository, a workspace, an index containing this
// repository — and then hands over to Claude Code with the exact words to
// use. Reviewing is reasoning, and reasoning belongs to the client; this
// command exists because the setup is what people get wrong, not the
// asking.
func Review(ctx context.Context, dir string, out io.Writer, launch bool) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("review: git is not on $PATH, and the branch under review is derived from it")
	}
	root, err := exec.CommandContext(ctx, "git", "-C", absDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("review: %s is not inside a git repository", absDir)
	}
	repoRoot := strings.TrimSpace(string(root))

	workspaceDir, ok := findWorkspaceUpward(absDir)
	if !ok {
		return fmt.Errorf("review: no indexed workspace at or above %s.\n"+
			"    cd <the directory holding your repositories>\n"+
			"    eng workspace create .\n"+
			"    eng workspace detach .\n"+
			"    eng workspace attach %s\n"+
			"    eng workspace attach /path/to/your-rules-repo", absDir, repoRoot)
	}

	store, err := openStore(workspaceDir, false)
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}
	_, attached, err := store.FindRepositoryByPath(ctx, repoRoot)
	if err != nil {
		store.Close()
		return fmt.Errorf("review: %w", err)
	}
	rules, err := store.ListDocumentsByType(ctx, domain.DocTypeRule)
	store.Close()
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}

	if !attached {
		return fmt.Errorf("review: %s is not attached to the workspace at %s, so a review here "+
			"would retrieve other repositories' documents.\n    cd %s && eng workspace attach %s",
			repoRoot, workspaceDir, workspaceDir, repoRoot)
	}

	fmt.Fprintf(out, "Repository: %s\n", repoRoot)
	fmt.Fprintf(out, "Workspace:  %s (%d rules indexed)\n\n", workspaceDir, len(rules))
	if len(rules) == 0 {
		fmt.Fprintln(out, "No rules are indexed. The review will run and will be told that nothing")
		fmt.Fprintf(out, "governs these files. Attach your rulebook first:\n    cd %s && eng workspace attach /path/to/your-rules-repo\n\n", workspaceDir)
	}

	// The command, not a description of it. Measured on this
	// organization's own repository: asked as "Review my current branch.",
	// another installed review skill claimed the request and Engineering
	// OS was called zero times in 37 tool calls; asked as /review-branch,
	// nine times in 59. See engineering-mcp/docs/reports/
	// TOOL_DISCOVERY_EXPERIMENT.md.
	fmt.Fprintln(out, "In Claude Code, type:")
	fmt.Fprintln(out, "\n    /review-branch")
	fmt.Fprintln(out, "\nType the command rather than describing the task. A plain-language request")
	fmt.Fprintln(out, "can be claimed by any other review skill installed here, and when it is,")
	fmt.Fprintln(out, "Engineering OS is never reached.")

	if !launch {
		return nil
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(out, "\n(The claude CLI is not on $PATH, so nothing was launched.)")
		return nil
	}
	fmt.Fprintln(out)
	cmd := exec.CommandContext(ctx, claude)
	cmd.Dir = repoRoot
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errSilent{err}
	}
	return nil
}
