package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

// declareRules implements `eng setup --rules-dir <dir>`: the
// single-repository answer to "where are your rules?".
//
// Two shapes of team exist and only one was served. A team with a
// separate rulebook names it with --rules, and their rules arrive
// already classified. A team with one repository holding handbook, docs,
// plans and code has nowhere to point --rules, and `eng taxonomy auto`
// does not close the gap: it classifies `handbook/` as Guide, which is
// what a handbook usually is, and the workspace still holds zero rules.
// Measured on exactly that layout — 6 documents indexed, 0 rules, and a
// review told that nothing governs the code.
//
// So this says it outright: this directory holds rules. It is a claim
// only the owner can make, which is why it is a flag they type and not
// something inferred.
//
// It never writes without confirmation, and it merges rather than
// replaces (RFC-0009): lines the repository already declares are kept
// exactly as written.
func declareRules(ctx context.Context, absDir string, dirs []string, in io.Reader, out io.Writer, assumeYes bool) error {
	if len(dirs) == 0 {
		return nil
	}

	// A container workspace has no single .engineering.yaml to write —
	// the taxonomy belongs to a repository, not to the directory that
	// happens to hold several. Refused rather than guessed: writing into
	// the container would produce a file no repository reads.
	if !isGitRepository(absDir) {
		return fmt.Errorf("setup: --rules-dir describes a directory inside one repository, but %s is a "+
			"container for several.\n    Run it inside the repository whose rules you are declaring:\n"+
			"        cd <that repository> && eng setup . --rules-dir %s", absDir, dirs[0])
	}

	patterns, err := rulePatterns(absDir, dirs)
	if err != nil {
		return err
	}

	path := filepath.Join(absDir, domain.TaxonomyFile)
	existing, hadFile, err := readTaxonomy(path)
	if err != nil {
		return err
	}

	merged, added := mergeRulePatterns(existing, patterns)
	if len(added) == 0 {
		fmt.Fprintf(out, "\n%s already declares %s as rules. Nothing to change.\n",
			domain.TaxonomyFile, strings.Join(patterns, ", "))
		return nil
	}

	fmt.Fprintf(out, "\nDeclaring where your rules live\n\n")
	for _, p := range added {
		fmt.Fprintf(out, "  %-24s → Rule\n", p)
	}
	verb := "Creating"
	if hadFile {
		verb = "Adding to"
	}
	fmt.Fprintf(out, "\n%s %s. Lines you already wrote are kept unchanged.\n", verb, path)

	if !assumeYes {
		ok, err := confirm(in, out, "\nWrite it?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintf(out, "Not written. %s is unchanged.\n", domain.TaxonomyFile)
			// Not an error. Declining is a legitimate answer, and failing
			// the whole setup over it would undo an install that worked.
			return nil
		}
	}

	if err := writeTaxonomy(path, renderTaxonomy(merged)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s\n", path)

	// Re-indexed here rather than left for the developer. The taxonomy
	// only takes effect on the next index, so stopping before it would
	// report success while the workspace still held zero rules — the
	// exact failure this flag exists to fix.
	fmt.Fprintln(out, "Re-indexing so the new mappings take effect...")
	if err := Index(ctx, absDir, out); err != nil {
		return err
	}
	return nil
}

// rulePatterns turns each --rules-dir into a taxonomy pattern, checking
// the directory exists. A typo that silently declared nothing would be
// indistinguishable from the problem it was meant to fix.
func rulePatterns(absDir string, dirs []string) ([]string, error) {
	patterns := make([]string, 0, len(dirs))
	for _, d := range dirs {
		clean := strings.TrimSpace(d)
		abs := clean
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(absDir, clean)
		}
		rel, err := filepath.Rel(absDir, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("setup: --rules-dir %s is not inside %s", d, absDir)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("setup: --rules-dir %s: %s is not a directory in this repository", d, rel)
		}
		patterns = append(patterns, filepath.ToSlash(rel)+"/**")
	}
	sort.Strings(patterns)
	return patterns, nil
}

// mergeRulePatterns adds the new patterns to what the file already
// declares, and reports which were actually new. A pattern the
// repository already declares keeps its own wording, whatever it says —
// this command adds a claim, it does not overrule one.
func mergeRulePatterns(existing domain.Taxonomy, patterns []string) (merged []domain.Mapping, added []string) {
	merged = append(merged, existing.Mappings()...)
	declared := map[string]bool{}
	for _, m := range merged {
		declared[m.Pattern] = true
	}
	for _, p := range patterns {
		if declared[p] {
			continue
		}
		declared[p] = true
		merged = append(merged, domain.Mapping{Pattern: p, Declared: "Rule", Type: domain.DocTypeRule})
		added = append(added, p)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Pattern < merged[j].Pattern })
	return merged, added
}

// renderTaxonomy writes the file, in the same shape `eng taxonomy auto`
// produces so the two are indistinguishable afterwards.
func renderTaxonomy(mappings []domain.Mapping) string {
	width := 0
	for _, m := range mappings {
		if n := len(m.Pattern) + 1; n > width {
			width = n
		}
	}
	var b strings.Builder
	b.WriteString("# What this repository's directories hold.\n")
	b.WriteString("# Edit freely — this file is yours.\n")
	b.WriteString("# A document's own `doc:` front matter always wins over these lines.\n")
	b.WriteString("taxonomy:\n")
	for _, m := range mappings {
		fmt.Fprintf(&b, "  %-*s %s\n", width, m.Pattern+":", m.Declared)
	}
	return b.String()
}
