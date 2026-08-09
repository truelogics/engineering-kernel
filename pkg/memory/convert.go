package memory

import (
	"github.com/truelogics/ai-memory/internal/domain"
	"github.com/truelogics/ai-memory/internal/kernel"
)

func toRepository(r domain.Repository) Repository {
	return Repository{
		ID:        r.ID,
		Name:      r.Name,
		LocalPath: r.LocalPath,
		RemoteURL: r.RemoteURL,
	}
}

func toIndexResult(r kernel.IndexResult) IndexResult {
	return IndexResult{
		Scanned:   r.Scanned,
		Added:     r.Added,
		Updated:   r.Updated,
		Unchanged: r.Unchanged,
		Deleted:   r.Deleted,
		Errors:    r.Errors,
		Failures:  toIndexFailures(r.Failures),
	}
}

func toIndexFailures(in []kernel.IndexFailure) []IndexFailure {
	if len(in) == 0 {
		return nil
	}
	out := make([]IndexFailure, len(in))
	for i, f := range in {
		out[i] = IndexFailure{Path: f.Path, Reason: f.Reason}
	}
	return out
}

// toContextPackage reshapes Retriever's labeled groups into
// ContextPackage's fixed fields (RFC-0004). Labels not recognized below
// (Architecture, Documentation, RFCs, Roadmap, ...) fold into
// RelevantFiles — ContextPackage deliberately doesn't distinguish them.
func toContextPackage(bundle kernel.RetrievalBundle, repoNames map[string]string) ContextPackage {
	pkg := ContextPackage{Task: bundle.Task}
	for _, group := range bundle.Groups {
		files := toFileContexts(group.Results, repoNames)
		switch group.Label {
		case "Related ADRs":
			pkg.ADRs = files
		case "Rules":
			pkg.Rules = files
		case "Related Issues":
			pkg.RelatedIssues = files
		case "Related PRs":
			pkg.RelatedPRs = files
		default:
			pkg.RelevantFiles = append(pkg.RelevantFiles, files...)
		}
	}
	return pkg
}

func toFileContexts(results []kernel.SearchResult, repoNames map[string]string) []FileContext {
	out := make([]FileContext, len(results))
	for i, r := range results {
		out[i] = FileContext{
			Path:       r.Document.Path,
			Repository: repoNames[r.Document.RepositoryID],
			Score:      r.Score,
			Snippet:    r.Snippet,
		}
	}
	return out
}
