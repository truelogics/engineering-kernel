package filesystem

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

var _ kernel.IncrementalCollector = (*Collector)(nil)

// CurrentRef implements kernel.IncrementalCollector: the repository's
// current commit, to record as the new LastIndexedCommit. Best-effort —
// empty string, nil error if repo isn't a git repository (or has no
// commits yet), so callers can treat "not incremental-capable" and "not
// git" the same way.
func (c *Collector) CurrentRef(ctx context.Context, repo domain.Repository) (string, error) {
	out, err := runGit(ctx, repo.LocalPath, "rev-parse", "HEAD")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// CollectChanged implements kernel.IncrementalCollector: git as the
// source of truth for what changed since sinceCommit, covering both
// committed changes (git diff) and working-tree changes (git status —
// staged, unstaged, and untracked), per RFC-0003's Trade-offs.
func (c *Collector) CollectChanged(ctx context.Context, repo domain.Repository, sinceCommit string) (changed []domain.RawDocument, deleted []string, err error) {
	if strings.TrimSpace(repo.LocalPath) == "" {
		return nil, nil, fmt.Errorf("filesystem: repository %q has no local path", repo.ID)
	}
	if strings.TrimSpace(sinceCommit) == "" {
		docs, err := c.Collect(ctx, repo)
		return docs, nil, err
	}

	statuses := map[string]byte{} // repo-relative path -> 'M' (add/modify) or 'D' (delete)

	committedOut, gitErr := runGit(ctx, repo.LocalPath, "diff", "--name-status", sinceCommit+"...HEAD", "--", "*.md", "*.markdown")
	if gitErr != nil {
		// sinceCommit may no longer exist (rebase/squash) or this may not
		// be a git repo at all — fall back to a full collect rather than
		// erroring `eng sync` outright.
		docs, err := c.Collect(ctx, repo)
		return docs, nil, err
	}
	parseNameStatus(committedOut, statuses)

	if wtOut, err := runGit(ctx, repo.LocalPath, "status", "--porcelain", "--", "*.md", "*.markdown"); err == nil {
		parsePorcelain(wtOut, statuses)
	}

	paths := make([]string, 0, len(statuses))
	for p := range statuses {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		if statuses[p] == 'D' {
			deleted = append(deleted, p)
			continue
		}
		full := filepath.Join(repo.LocalPath, filepath.FromSlash(p))
		content, readErr := os.ReadFile(full)
		if readErr != nil {
			return nil, nil, readErr
		}
		raw, rawErr := domain.NewRawDocument(repo.ID, p, content)
		if rawErr != nil {
			return nil, nil, rawErr
		}
		changed = append(changed, raw)
	}
	return changed, deleted, nil
}

// parseNameStatus parses `git diff --name-status` output into path ->
// status ('M' add/modify, 'D' delete). A rename is a delete of the old
// path plus an add of the new one.
func parseNameStatus(output string, out map[string]byte) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		code := fields[0]
		switch {
		case strings.HasPrefix(code, "R") && len(fields) >= 3:
			out[filepath.ToSlash(fields[1])] = 'D'
			out[filepath.ToSlash(fields[2])] = 'M'
		case strings.HasPrefix(code, "D"):
			out[filepath.ToSlash(fields[1])] = 'D'
		default: // A, M, C, ...
			out[filepath.ToSlash(fields[1])] = 'M'
		}
	}
}

// parsePorcelain parses `git status --porcelain` (v1 format: two status
// chars, a space, then the path — or "old -> new" for renames) the same
// way: path -> 'M' or 'D'. Untracked ("??") counts as 'M' — a new file
// needs collecting, same as a modification.
func parsePorcelain(output string, out map[string]byte) {
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		rest := strings.TrimSpace(line[3:])
		if idx := strings.Index(rest, " -> "); idx != -1 {
			out[filepath.ToSlash(rest[:idx])] = 'D'
			out[filepath.ToSlash(rest[idx+4:])] = 'M'
			continue
		}
		path := filepath.ToSlash(rest)
		if strings.Contains(code, "D") {
			out[path] = 'D'
		} else {
			out[path] = 'M'
		}
	}
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
