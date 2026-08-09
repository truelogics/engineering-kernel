package filesystem

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// dropIgnored removes paths the repository itself declares are not part
// of it, by asking git.
//
// A repository's knowledge is what the repository tracks. Skipping a
// fixed list of directory names cannot express that: it catches
// node_modules and vendor and misses everything else a project ignores.
// Measured on a real 12,500-file monorepo (Sprint 12 dogfooding), the
// fixed list let through 22,445 markdown files where 726 were tracked —
// 21,690 of them tool caches under .claude/ — and indexing had not
// finished after ten minutes.
//
// Deliberately advisory: if root is not a git repository, or git is not
// installed, or the command fails for any reason, every path is kept and
// SkipDirs remains the only filter. Silently indexing less than asked
// would be the worse failure, and this is an optimization of *scope*,
// not a correctness guarantee (engineering:rules/no-silent-fallback.md
// governs substituting a fake result, which returning the full list is
// not).
func dropIgnored(ctx context.Context, root string, paths []string) []string {
	if len(paths) == 0 {
		return paths
	}
	ignored, ok := gitIgnored(ctx, root, paths)
	if !ok || len(ignored) == 0 {
		return paths
	}
	kept := paths[:0:0]
	for _, p := range paths {
		if !ignored[p] {
			kept = append(kept, p)
		}
	}
	return kept
}

// gitIgnored returns the subset of paths git reports as ignored. ok is
// false when git could not answer, which the caller treats as "ignore
// nothing".
func gitIgnored(ctx context.Context, root string, paths []string) (map[string]bool, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false
	}

	// One process for the whole walk, not one per file: check-ignore
	// reads newline-separated paths on stdin with --stdin.
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(p)
		in.WriteByte('\n')
	}

	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "--stdin")
	cmd.Stdin = &in
	var out bytes.Buffer
	cmd.Stdout = &out

	// check-ignore exits 1 when nothing matched and 128 when root is not
	// a repository. Only 0 and 1 carry a usable answer.
	err := cmd.Run()
	if code, ok := exitCode(err); !ok || (code != 0 && code != 1) {
		return nil, false
	}

	ignored := make(map[string]bool)
	for _, line := range strings.Split(out.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ignored[normalize(root, line)] = true
		}
	}
	return ignored, true
}

// normalize maps a path git printed back onto the absolute form the
// walk produced, so the two can be compared. git echoes what it was
// given, but resolves relative to -C.
func normalize(root, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(root, p)
}

func exitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}
